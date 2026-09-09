// Package app holds dockhand's operations. One exported type per
// operation, each with the dependencies that operation actually uses,
// a Run method, and a typed result. It replaces the engine facade and
// the sequencing that leaked into the CLI.
//
// Operations own sequence; lifecycles own stages. Nothing here writes a
// record: every stage below is a function exported by change, run,
// lease or publish, and a cross-lifecycle write is two of those in one
// statestore.Amend. Nine operations: Change, Survey, Verify, Accept,
// Promote, Cancel, Discard, Cycle, Status. hold, unhold and dismiss are
// ENTRY POINTS here rather than operations: each opens one Amend and
// calls one change mutator, because cli may never import a lifecycle
// package (R22 as ruled 2026-09-07 — the principle says what earns an
// OPERATION, not that cli may reach past app).
//
// NO OPERATION OPENS A FILE, TAKES A LOCK, READS AN ENVIRONMENT
// VARIABLE OR NAMES A VERB. Every one of those is a dependency passed
// in as a value: the clock is a func, the provider is a func because a
// machine with no tart is not an error, residency is a value on the
// operations that do not wait and a func on the ones that do, and the
// two lockfiles the design names are held by cli around the operation.
// The one thing that looks like an exception is git, and it is not one:
// a repository handle is a value the composition root opened, and the
// refs it moves move only through statestore's batch (R23).
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// ErrNothingPrepared is the refusal of a request whose Prepared names no
// subject at all. It is a sentinel because it is a wiring gap — cli
// plans and prepares before it builds a ChangeRequest, so an empty
// subject list is a composition mistake and not a decline a person can
// act on — and because rule 7 forbids reading the empty slice as "a
// change about nothing".
var ErrNothingPrepared = errors.New("app: the request carries no prepared subject")

// Delivery is how far an operation carries a change. FIVE values (R7):
// Gated is deleted with the app.gated method that spelled it out, and
// the two refusals that depended on it. Gated meant the invoking process
// owned a build; dispatch means the daemon does; both cannot be true
// without either bypassing the queue or a blocking client — two judges.
// How long the caller stays is a separate question, ChangeRequest.Wait,
// and not a delivery.
//
// WHETHER A BUILD IS ASKED FOR IS ALSO NOT A DELIVERY, and it used to
// be: --no-verify was the "depth flag" and it chose Branch, so a person
// who wanted a pull request without a build had no way to say it and the
// two flags were refused together as a contradiction. They are not one:
// --to-pr says WHERE the change is bound and --no-verify says HOW MUCH
// EVIDENCE goes with it, and every combination of the two is meaningful.
// So the build question moved to ChangeRequest.Unverified and Delivery
// kept the destinations — the same separation Wait already has, and for
// the same reason. Branch remains the destination a change takes when
// nothing is asked to carry it further; it no longer MEANS "no build",
// it merely has nowhere to put one.
type Delivery uint8

const (
	Document    Delivery = iota // --plan / --diff: emit and stop
	InPlace                     // --in-place: edit the working tree; stop after prepare
	Branch                      // --no-verify: mint and stop
	Enqueue                     // default: mint, enqueue durably, try to start, detach always
	PullRequest                 // --to-pr: Destination ToPublished on the record; Change never publishes
)

// Needs is the set of services an invocation will actually use,
// computed by the composition root from the WHOLE parsed request.
type Needs struct {
	Repo, Forge, Verifier, Evaluator, Fetcher, Tree bool
}

// Needs answers what this request will use, so the composition root can
// acquire exactly that and close it in reverse order.
//
// FOUR OF THE SIX FALL OUT OF THE DELIVERY AND TWO DO NOT, which is why
// ChangeRequest carries Fetches and why this is a method rather than a
// table in cli. A repository, an evaluator and a tree are needed by
// every road: even --plan resolves the target against the tree, asks the
// port what it evaluates to, and reads the base commit's Portfile out of
// git, because a plan is made against the bytes a commit would land on
// and not against the working copy. A verifier is needed exactly where
// an attempt may be started, and a forge exactly where a publication
// may be opened. Whether the network is read is a fact about the INTENT
// — a revision bump downloads nothing — so it rides on the request from
// the catalogue entry cli chose, and this method reports it rather than
// deriving it from a delivery that cannot know.
func (r ChangeRequest) Needs() Needs {
	return Needs{
		Repo:      true,
		Tree:      true,
		Evaluator: true,
		Fetcher:   r.Fetches,
		Verifier:  (r.Delivery == Enqueue || r.Delivery == PullRequest) && !r.Unverified,
		Forge:     r.Delivery == PullRequest,
	}
}

// Residency is what cli read off $GIT_COMMON_DIR/.dockhand-dispatch.lock
// — R10's chooser, and the input to every remedy line in the 60 band
// (R13, R14). Three states, because the lock can be unreadable: a
// permission error or a foreign filesystem is not "no dispatcher", and a
// process that assumed so would appoint itself judge beside a
// dispatcher it could not see. Under Unknown a --timeout watches only, and
// says so; Status settles nothing and says why (rule 7).
//
// THE PROBE IS A SHARED LOCK, and the reason it is not a try-lock is a
// defect an adversarial pass found in the draft that said "try-lock the
// lockfile": a try-lock TAKES the lock, so for the instant a bump,
// verify, accept or status probed, it WAS the resident. A concurrent
// probe read "resident, holder = <the other verb's pid>" and printed
// "dispatch (pid N) will start it" for a pid that would exit in 200ms; a
// --timeout entering that iteration dropped to watching with nobody
// judging; and a `dockhand dispatch` starting at that instant failed its
// own lock and exited 0 believing a scheduler was up, when the holder
// was a `status`. So: dispatch holds LOCK_EX for its whole life;
// probers take LOCK_SH|LOCK_NB. Success means no exclusive holder
// (NoDispatcher) and the shared lock is dropped at once; EWOULDBLOCK
// means DispatcherResident, and the stamp is read for Holder and Since;
// any other errno is ResidencyUnknown. Probers never exclude each other.
// dispatch acquires LOCK_EX with a short bounded retry, so a passing
// shared probe cannot make it exit, and "exits 0 naming the holder" is
// said only when the stamp names a dispatcher. `status --no-update`
// takes no lock at all, reports ResidencyUnknown, and prints no remedy
// line. The lock and the probe live in cli (R2); app receives the
// value, or a function that re-reads it.
//
// It is re-read each iteration of a wait, because a dispatcher that
// appears mid-wait takes over and the waiting process drops to watching
// — one judge per job, chosen by residency, and residency changes.
type Residency struct {
	State  ResidencyState
	Holder record.OwnerID // the dispatcher, when Resident
	Since  time.Time      // when it took the lock, off the stamp
}

// ResidencyState is the three answers the probe can give. Its zero is
// ResidencyUnknown on purpose: a caller that never probed has not
// learned that nobody is resident, and a zero that meant "no dispatcher"
// would appoint every unprobed process the judge of somebody else's job.
type ResidencyState uint8

const (
	ResidencyUnknown   ResidencyState = iota // the lock could not be read
	NoDispatcher                             // this process is the judge under --timeout
	DispatcherResident                       // watch the record; it judges
)

// Change is the operation behind bump, bump-revision and
// refresh-checksums with ONE target. Its road is clean-slate's, kept
// whole: resolve, plan, prepare, commit, ONE Amend (change.MintIn +
// run.EnqueueIn; the batch creates the branch), resolve, start ONCE,
// detach.
type Change struct {
	Repo   *git.Repo
	Ledger *ledger.Ledger
	State  *statestore.Store
	// portdir from the commit the attempt names. Change holds one even
	// though it just prepared those files in memory, because the guest is
	// seated from the RECORD on every road — this try, cycle's drain and a
	// later verify all call the same Start over the same Stager — and a
	// bump that staged from its own memory would be the one road that
	// could disagree with the drain about what it built.
	Stage run.Stager
	// Local is the propose step's seam, carried because the waiting judge
	// under --timeout calls run.Finish and Finish proposes.
	Local run.Local
	// Verifier is a function and every other dependency is a value,
	// because a machine with no tart is not an error. Its ABSENCE is
	// asked BEFORE the mint Amend — a presence check, never a vacancy ask
	// (R8 untouched) — and answered by minting without enqueueing: the
	// branch exists, the record says unverified, the exit is 0 with an
	// advisory naming `provision` and `dockhand verify`, and --to-pr's
	// Promote follows as sequenced. A draft left a permanently Queued
	// attempt behind and exited 61, where the shipped tool narrows the
	// contract and records an unverified branch; 61 is now only
	// ErrNoEnvironment — a provider with no base for this release — which
	// a `provision` will fix and a queued attempt should wait for.
	//
	// A NIL FUNCTION IS "no provider", and that is not a wiring gap being
	// read as a fact. cli acquires a verifier exactly where Needs says the
	// delivery may start one; a Document or InPlace road wires none, and a
	// host with no tart wires none either. Both mean the same thing to
	// every road below, so nil answers with verify.ErrNoProvider instead
	// of panicking on a call nobody made.
	Verifier func(context.Context) (verify.Verifier, error)
	// Me is who this process is — EnqueuedBy on the attempt, Owner on the
	// lease. Passed in rather than derived: record.OwnerID.Root is
	// canonical at exactly one point, and that point is cli.
	Me record.OwnerID
	// Residency re-reads the dispatch lock. It is a FUNCTION where
	// ChangeRequest.Residency is a value, for the reason Verifier is one:
	// the value is what cli read once for the remedy line, and a wait
	// that lasts an hour must notice a dispatcher that appeared at minute
	// ten and drop to watching. A dependency is a value unless its answer
	// legitimately changes under the operation; this one does. It takes a
	// context because the probe is I/O on a lockfile, and it is supplied
	// by cli over that file (R2: app opens no lock). Nil means "never
	// re-read", which is what a caller without a lock path wants.
	Residency func(context.Context) Residency
	Now       func() time.Time
	Progress  progress.Sink
}

// ChangeRequest is what one invocation asked for. Trace is GONE (R11:
// watching a build is `dockhand log --trace`'s job); Wait and Residency
// arrived with it. Platform is singular — bump's --on takes one release
// — and the invocation-level plural lives on VerifyRequest.Platforms and
// nowhere else.
type ChangeRequest struct {
	Prepared change.Prepared
	Delivery Delivery
	Platform platform.Release
	Test     bool
	KeepEnv  bool
	Replace  InFlight
	Prov     change.Provenance
	// Slug and Riders are the PLAN's own naming, and they are on the
	// request because change.Prepared does not carry them and
	// change.Minting requires both: Slug is what the branch is named
	// after (git.MintBranchName adds nothing but the namespace) and
	// Riders is the housekeeping vocabulary a note and a pull request
	// body read back. A slug cannot be recomposed from a Prepared —
	// "jq-1.8.2", "jq-checksums" and "jq-housekeeping" are the intent's
	// words about its own change — so the value travels from the planner
	// that named it rather than being guessed at here.
	Slug   string
	Riders []string
	// Fetches says the intent behind this request reads the network, off
	// the catalogue entry cli chose. It is here for Needs and for nothing
	// else: no road below branches on it.
	Fetches bool
	// Unverified says this request asks for NO BUILD AT ALL, which is
	// --no-verify. It is separate from Delivery because it is a separate
	// question: --no-verify --to-pr is a change carried to a pull request
	// with nothing behind it but the person who typed it, and that is a
	// road, not a contradiction. See the Delivery doc.
	Unverified bool
	// Wait is nil to detach at once (the default), or how long this
	// caller stays. THREE VALUES, not two: nil detaches; a positive
	// duration waits and REAPS the run at expiry (record.InterruptTimeout
	// — the build is stopped and its guest kept); zero waits with no
	// deadline at all, which is what --to-pr asks for when no --timeout
	// was given, because a person who asked for a pull request asked for
	// the thing that authorizes it.
	//
	// Expiry used to DETACH and never fail. It reaps now because that is
	// what the word means and what people assume it means: a --timeout
	// that quietly let an unwanted build run to completion would be a
	// deadline in name only. What it costs is written where it is paid —
	// a reaped run has no pass, so nothing downstream will publish it
	// without a person saying so again.
	Wait      *time.Duration
	Residency Residency
}

// InFlight is what to do about a branch already standing for this port.
// Refuse is exit 11; Replace supersedes the old change under the new
// one; Advance and Supersede are Survey's answers.
type InFlight uint8

const (
	Refuse InFlight = iota
	Replace
	Advance   // a selector meeting its own branch: leave it alone, a Stood row
	Supersede // a selector minting beside it: change.SupersedeIn or CloseIn(old, Superseded) in the same Amend
)

// Result is what happened to ONE change. Returned beside an error, not
// through one, because a queued attempt and a failed start are both a
// branch that now exists.
//
// It carries the ATTEMPT, the durable id every remedy line names, and
// the LEASE, empty until something started — and it no longer carries
// Proof (the gate's value; the verdict is on the attempt), Published or
// Advisories (Promote's, moved to PromoteResult with the --to-pr
// delegation). Exit is DERIVED from Did and Deferred — Started 0, Queued
// 60, Queued with NoEnvironment 61, Minted with NoProvider 0 (an
// unverified branch and an advisory), Stood the verdict band — and never
// from wrapped sentinels: app classifies once at its boundary with run,
// and nothing downstream touches a provider error.
type Result struct {
	Did Realization
	Ref change.Ref
	// Attempt is the attempt's durable id, empty when nothing was
	// enqueued. Every remedy line in the 60 band names it.
	Attempt string
	// Lease is the lease REQUEST token the attempt names — the id the
	// store keys a lease by and the one name a lease is guaranteed to
	// have — and it is empty until something started. The sketch types
	// this as a *record.LeaseID; that value is the PROVIDER's name for
	// the environment, it is not on the attempt run.Start hands back, and
	// reading it would cost a second read of the whole store per bump and
	// per swept target to fill a field a report can join on this token
	// instead. Empty means "nothing started it", and only that.
	Lease      string
	Verdict    record.RunState // set only under Stood
	Deferred   *Deferral
	Superseded []string
	Owed       []Obligation
}

// ErrMintedSubmitErrored is the identity of the one half-done outcome a
// change road can leave: the branch is minted, its attempt is enqueued,
// and the submission to the verifier then failed. A sentinel beside
// MintError, so a caller may ask errors.Is without knowing the type.
var ErrMintedSubmitErrored = errors.New("app: the branch was minted and its verification could not be submitted")

// MintError is that outcome typed with the two names a caller needs —
// the branch that now exists and the attempt left queued on it — because
// the half that stands is not recoverable from the words underneath,
// which are whatever the provider said (rule 6).
//
// IT CARRIES NO EXIT BAND. cli's classifier numbers it, the way it
// numbers every other identity: what a half-done mint MEANS to a shell
// is the command line's contract, and a Result that reported success
// beside an error would say the opposite of what happened. The road
// returns this instead of the raw provider failure so a wrapper can tell
// "the branch exists, submit again" from "nothing was written".
type MintError struct {
	Branch  string
	Attempt string
	Err     error
}

func (e *MintError) Error() string {
	return fmt.Sprintf("minted %s, and attempt %s could not be submitted — the branch stands: %v",
		e.Branch, e.Attempt, e.Err)
}

// Unwrap returns the identity a caller branches on and the cause
// underneath it.
func (e *MintError) Unwrap() []error { return []error{ErrMintedSubmitErrored, e.Err} }

// Exit is the band Result lands in. The verdict codes are the shipped
// exitcode table's (70 failed, 71 blocked, 72 unsupported, 73 errored
// or canceled); they are written out here because this is the ONE
// place a Result becomes a code, and a table a reader can see beats an
// errors.As over a sentinel a caller forgot to wrap.
func (r Result) Exit() int {
	switch r.Did {
	case Started, Minted, Shown, Edited, NothingToDo:
		return 0
	case Queued:
		if r.Deferred != nil {
			switch r.Deferred.Reason {
			case NoProvider, NoEnvironment:
				return 61
			case DeferredUnknown, Unsupported, ProviderError:
				return 60
			}
		}
		return 60
	case Stood:
		switch r.Verdict {
		case record.Passed:
			return 0
		case record.Failed:
			return 70
		case record.Blocked:
			return 71
		case record.Unsupported:
			return 72
		case record.Errored, record.Canceled, record.Superseded:
			// "the verification ended without concluding anything", which
			// is what 73 is for and what a caller waiting on one needs to
			// hear; the twin's reason says which of the three it was.
			return 73
		case record.Faulted:
			// The FOURTH way, and its own code, because the remedy is not
			// 73's: nothing is wrong with this machine and retrying will
			// reproduce it exactly. A script that treats it as 73 waits
			// forever for a guest that was never the problem.
			return 74
		case record.Queued, record.Submitting, record.Running, record.Withheld:
			// A Stood result holding an unfinished or unseated run is an
			// incident and not a verdict: the road said it stayed for the
			// answer and the record does not have one. It lands in 73 with
			// the rest of "ended without concluding", rather than in the
			// band of last resort, because the caller was waiting on a
			// verification and that is what did not arrive.
			return 73
		}
		return 73
	case NotRealized:
		return 1
	}
	return 1
}

// Realization is how far the road got. Queued and Started sit between
// Minted and Stood because the exit band turns on exactly that
// distinction (R14): a bump that starts a build exits 0 and one that
// only queues exits 60, with a dispatcher and without.
type Realization uint8

const (
	NotRealized Realization = iota
	NothingToDo
	Shown
	Edited
	Minted  // --no-verify, or no provider on this host: a branch, no attempt
	Queued  // an attempt written; nothing started it yet
	Started // an attempt running; no verdict — it arrives via status
	Stood   // --timeout stayed for the verdict
)

// Deferral says why no verification started, in a form a caller can act
// on. NoVacancy is NOT a reason any more: a full machine leaves the
// attempt Queued with no deferral at all (exit 60), because nothing is
// wrong and nothing needs a person. NoProvider arrives on a MINTED
// result, not a queued one: nothing was enqueued, and the deferral is
// the advisory. What remains here is what a pass will not fix by
// itself.
type Deferral struct {
	Reason DeferralReason
	Detail string
}

// DeferralReason is typed for rule 6: the remedy differs per reason —
// install a provider, provision a base image, ask for a platform this
// backend answers — and a caller must branch on identity rather than on
// the sentence Detail carries.
type DeferralReason uint8

const (
	DeferredUnknown DeferralReason = iota
	NoProvider
	NoEnvironment
	Unsupported
	ProviderError
)

// Obligation is work this operation left owing.
type Obligation struct {
	What string
	Why  string
}

// Claimant is who this invocation is, for the records it writes before
// the calls that can outlive it.
func (c Change) Claimant() lease.Claimant { return lease.Claimant{Owner: c.Me} }

// Run performs the Change road. The body is written out because THE
// ORDER IS THE DESIGN CLAIM: the old change released, the new record,
// its attempt and its branch are ONE Amend and ONE batch; the Ref app
// returns is resolved afterwards, never assembled.
//
// Two things can have happened to `old` between the road's read and its
// commit, and they are told apart by two judges (rule 2): a PEER — a
// retire pass, a `bump --replace` in another worktree — closed or
// superseded it, which change.CloseIn/SupersedeIn refuse INSIDE the
// closure over tx.State() as change.ErrNotBound (exit 11: re-resolve
// and rerun; the rerun finds no standing change, or the peer's); or a
// HAND moved or deleted its branch, which the batch refuses as
// git.ErrRefMoved (exit 45). Standing answers from the store; the
// road's own Resolve of old.Branch below is kept — cancel.run no longer
// needs its Ref, but the road wants the early finding — and reports a
// hand's move before any work is stopped, and the batch asks the same
// question again at commit (one question, two moments, one code, 45).
// With the record-level refusal in front of it, that judge is reserved
// for a hand: without ErrNotBound the closure would restamp a
// peer-closed record and queue a delete line for a branch the peer
// already removed, and the person would be told their own git had moved
// it.
func (c Change) Run(ctx context.Context, r ChangeRequest) (Result, error) {
	if len(r.Prepared.Subjects) == 0 {
		return Result{}, ErrNothingPrepared
	}
	// resolve: a standing change for this port is exit 11 under Refuse;
	// under Replace it is superseded in the mint Amend below, after the
	// Cancel operation's stages have stopped its live work.
	st, err := readBeforeMint(ctx, c.State)
	if err != nil {
		return Result{}, err
	}
	old, hasOld := change.Standing(st, r.Prepared.Subjects[0].Port)
	if hasOld && r.Replace == Refuse {
		return Result{}, change.ErrInFlight
	}
	// plan and prepare happened in cli through planning.Planner and
	// change.Prepare; r.Prepared is the result. Document and InPlace stop
	// here.
	switch r.Delivery {
	case Document:
		return Result{Did: Shown}, nil
	case InPlace:
		return Result{Did: Edited}, nil
	case Branch, Enqueue, PullRequest:
		// the mint roads; they continue below.
	}
	// presence, BEFORE the mint Amend: no provider means mint without
	// enqueue. Not a vacancy ask — R8 stands — a presence check.
	prov, provErr := provider(ctx, c.Verifier)
	enqueue := r.Delivery != Branch && !r.Unverified && !isNoProvider(provErr)
	if hasOld && r.Replace == Replace {
		if _, err := change.Resolve(ctx, c.Repo, c.State, old.Branch); err != nil {
			return Result{}, err // a hand moved it: 45, before any work is stopped
		}
		cancel := Cancel{Repo: c.Repo, Ledger: c.Ledger, State: c.State, Verifier: c.Verifier, Local: c.Local, Me: c.Me, Now: c.Now, Progress: c.Progress}
		if _, err := cancel.run(ctx, st, old.ID, old.Tip); err != nil {
			return Result{}, err
		}
		wt, err := c.Repo.CheckedOutAt(ctx, old.Branch)
		if err != nil {
			return Result{}, err // could not read the worktree list: band 1, its own words (rule 7)
		}
		if wt != "" {
			return Result{}, change.ErrCheckedOut // 46: switch away first
		}
	}
	// A BRANCH NOTHING OWNS IS NAMED, NOT COLLIDED WITH. MintIn's
	// ErrStanding is the record-level half of a pair whose ref-level half
	// is the create line, and its own doc says both are needed because
	// "a record binds the name; a foreign ref stands at it". There is a
	// THIRD case neither names, and it is dockhand's own leavings:
	// retirement closes a change without necessarily taking its branch,
	// so a rejected pull request leaves a branch with no live change.
	//
	// That reached the ref-level judge and came back as
	//   git: refs/heads/dockhand/skim-5.7.0 is at "f45b30ab", expected ""
	// — exit 45, "your own git moved this", reported as a foreign hand
	// when the hand was dockhand's. Measured in the field, and it made
	// the port unbumpable until the whole store was purged.
	//
	// The record knows better, so it says so, and names the verb that
	// now clears it.
	if !hasOld && c.Repo != nil && r.Slug != "" {
		branch := branchFor(r.Slug)
		if c.Repo.HasBranch(ctx, branch) {
			return Result{}, fmt.Errorf("%w: %s stands with no change behind it, left by one that closed; `dockhand discard %s` removes it",
				change.ErrOrphanBranch, branch, branch)
		}
	}

	// commit: an unreferenced object over the base.
	sha, content, err := change.Commit(ctx, c.Repo, r.Prepared, r.Prepared.Base.Sha)
	if err != nil {
		return Result{}, err
	}
	id := newChangeID()
	m := change.Minting{
		ID: id, Branch: branchFor(r.Slug), Tip: sha, Content: content,
		Subjects: r.Prepared.Subjects, Crossing: change.Cross(r.Prepared),
		Destination: destination(r.Delivery), Unverified: r.Unverified, Closes: r.Prepared.Closes,
		Findings: r.Prepared.Findings, Riders: r.Riders, Slug: r.Slug,
		Base: r.Prepared.Base, Prov: r.Prov,
	}
	// ONE Amend, in this order: the old change released (SupersedeIn
	// while its publication is open, else CloseIn Superseded), its
	// queued attempts withdrawn and its local branch's delete line
	// queued; MintIn, whose ErrStanding check now meets a free name and
	// whose create line lands in the same batch; EnqueueIn unless
	// --no-verify or no provider. Adoptable is asked over tx.State() and
	// its answer is returned as DATA; the closure decides nothing else and
	// performs no effect — a tx.Ref line is data the commit carries.
	var att record.Attempt
	var adopted bool
	spec := run.Spec{
		Content: content, Roster: rosterOf(r.Prepared.Subjects),
		FromSource: fromSourceOf(r.Prepared.Subjects),
		Platform:   r.Platform, Test: r.Test, KeepEnv: r.KeepEnv,
	}
	err = c.State.Amend(ctx, func(tx *statestore.Txn) error {
		att, adopted = record.Attempt{}, false
		if hasOld && r.Replace == Replace {
			if err := supersedeIn(tx, old, id, c.Me, c.Now()); err != nil {
				return err
			}
		}
		if _, err := change.MintIn(tx, m, c.Now()); err != nil {
			return err
		}
		if !enqueue {
			return nil
		}
		if a, ok := run.Adoptable(attempts(tx.State()), content, spec.ID(), r.Platform, c.Now()); ok {
			att, adopted = a, true
			return nil
		}
		a, err := run.EnqueueIn(tx, run.Enqueue{
			Change: id, Sha: sha, Content: content, Spec: spec, Platform: r.Platform,
			Ask: record.Ask{Test: r.Test, KeepEnv: r.KeepEnv}, EnqueuedBy: c.Me,
		}, c.Now())
		att = a
		return err
	})
	if err != nil {
		return Result{Did: NotRealized}, err // nothing was written, the ref included
	}
	// the note, over the state this road leaves: the commit exists now, so
	// every return below is a return a reviewer can read with `git log
	// --notes` — the --no-verify branch included, which is the road that
	// carried no note at all.
	defer func() { exportNote(ctx, c.State, c.Ledger, sha, c.Progress) }()
	// resolve: the batch created the branch; app holds no Ref it did not
	// resolve. A foreign move in the second between is reported here.
	ref, err := change.Resolve(ctx, c.Repo, c.State, m.Branch)
	if err != nil {
		return Result{Did: Minted}, err
	}
	res := Result{Did: Minted, Ref: ref}
	if !enqueue {
		if isNoProvider(provErr) {
			res.Deferred = &Deferral{Reason: NoProvider, Detail: "unverified; install tart and `dockhand verify`"}
		}
		return res, nil
	}
	res.Did, res.Attempt = Queued, att.ID
	if adopted {
		// announced by app, after the Amend, from the returned attempt —
		// never from inside the closure.
		say(c.Progress, progress.Info, "adopted attempt "+att.ID+" started "+att.Started.Format(time.RFC3339))
		// AND AN ADOPTEE THAT IS NOT QUEUED IS NOT STARTED. Adoption
		// draws from three states and run.Start takes one; the two this
		// road existed to make free were the two it turned into a mint
		// error naming a branch that was fine.
		if did, done := resumed(att); done {
			res.Did = did
			if did == Started {
				res.Lease = leaseOf(att)
			}
			if did == Stood {
				res.Verdict = verdictOf(att)
				return res, nil
			}
			if r.Wait == nil {
				return res, nil
			}
			final, werr := watch(ctx, c.State, c.Ledger, prov, c.Local, att, spec, *r.Wait, r.Residency, c.Residency, c.Claimant(), c.Now, c.Progress)
			if werr != nil {
				return res, werr
			}
			if final.Phase == record.Finished {
				res.Did, res.Verdict = Stood, verdictOf(final)
			}
			return res, nil
		}
	}
	// start ONCE, the sequencer. ErrNoVacancy: nothing written on the
	// attempt, stays Queued, exit 60. ErrNoEnvironment: stays Queued with
	// a typed Deferral, exit 61.
	if provErr != nil {
		res.Deferred = &Deferral{Reason: ProviderError, Detail: provErr.Error()}
		return res, nil
	}
	started, err := run.Start(ctx, c.State, prov, c.Stage, att, c.Claimant(), c.Now())
	switch {
	case err == nil:
		res.Did, res.Lease = Started, leaseOf(started)
	case isNoVacancy(err):
		// stays Queued
	case isNoEnvironment(err):
		res.Deferred = &Deferral{Reason: NoEnvironment, Detail: err.Error()}
	default:
		// THE HALF THAT STANDS IS NAMED. Everything before this line
		// committed: the branch exists, the attempt is queued on it, and
		// only the submission failed. Handed up raw, that arrived at a
		// shell as the band of last resort, which told a wrapper nothing
		// happened while a branch it will meet again sat in the checkout.
		return res, &MintError{Branch: m.Branch, Attempt: att.ID, Err: err}
	}
	if r.Wait == nil || res.Did != Started {
		return res, nil
	}
	// watch: by residency, re-read each iteration.
	final, err := watch(ctx, c.State, c.Ledger, prov, c.Local, started, spec, *r.Wait, r.Residency, c.Residency, c.Claimant(), c.Now, c.Progress)
	if err != nil {
		return res, err
	}
	if final.Phase == record.Finished {
		res.Did, res.Verdict = Stood, verdictOf(final)
	}
	return res, nil
}

// supersedeIn is the closure step every replace and every Survey
// supersede runs FIRST, before the new change's MintIn: SupersedeIn
// while the old change's publication is open (it stays open and
// superseded; Cycle's close stage retires it from the forge's word),
// CloseIn(Superseded, by new) otherwise; then run.WithdrawIn over the
// old change's queued attempts, so the drain never meets them; then
// change.DemolishIn of the old LOCAL branch — its delete line, old = the
// record's Tip, in the same batch as the new branch's create line.
// Three or four owned mutators, one transaction; the fork copy stays
// with the shipped advisory while its pull request is open.
func supersedeIn(tx *statestore.Txn, old record.Change, by record.ChangeID, me record.OwnerID, now time.Time) error {
	var err error
	if publicationOpen(tx.State(), old.ID) {
		err = change.SupersedeIn(tx, old.ID, by, now)
	} else {
		err = change.CloseIn(tx, old.ID, record.ChangeSuperseded, by, now)
	}
	if err != nil {
		return err
	}
	run.WithdrawIn(tx, old.ID, record.InterruptSuperseded, me, now)
	if old.Branch == "" {
		return nil // a snapshot has no branch to demolish; its pin is CloseIn's
	}
	return change.DemolishIn(tx, old.ID, now)
}

// watch is the one wait loop, shared by Change, Verify and Accept.
// Residency chooses the role: resident -> run.AwaitRecord (a watcher);
// none -> this process is the judge and loops run.Finish over ITS OWN
// attempt until terminal; Unknown -> watch only, and say so. It re-reads
// residency each iteration so a dispatcher that appears mid-wait takes
// over. Neither role polls the provider outside Finish (R10).
func watch(ctx context.Context, st *statestore.Store, l *ledger.Ledger, prov verify.Verifier, local run.Local, a record.Attempt, spec run.Spec, wait time.Duration, res Residency, reread func(context.Context) Residency, by lease.Claimant, now func() time.Time, p progress.Sink) (record.Attempt, error) {
	// A BLOCKING INVOCATION SAYS WHAT IT IS BLOCKING ON. Everything else
	// about this tool is built so that walking away is free, so the one
	// road that asks a person to stay owes them the reason, the scale and
	// the way out — a terminal that has printed nothing for forty minutes
	// is indistinguishable from one that has hung, and the person who
	// cannot tell reaches for the thing that loses their work.
	say(p, progress.Info, waiting(spec, wait))
	// THE PARENT IS KEPT because the deadline is not the only way out of
	// the loop and the two ways mean opposite things. A deadline that
	// passes is this caller's --timeout and reaps; a parent that is
	// canceled is the PERSON — Ctrl-C, or a signal cli forwarded — and
	// reaping there would turn "stop showing me this" into "throw the
	// build away", which is the one thing an interrupted wait must not
	// do. Walking away stays free.
	parent := ctx
	if wait > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, wait)
		defer cancel()
	}
	// A zero wait is UNBOUNDED and not "expire at once": --to-pr with no
	// --timeout asks to stay until the thing that authorizes the pull
	// request exists. The loop then ends on the verdict or on the person.
	for a.Phase != record.Finished && ctx.Err() == nil {
		// THE RE-READ IS FIRST, and it used to be last. The claim it
		// exists to make — "a dispatcher that appeared mid-wait takes
		// over" — held in exactly one direction, because AwaitRecord does
		// not return on a timer: it loops internally until the attempt
		// SETTLES. So the re-read sat after a call that only returns once
		// the work is already done.
		//
		// A dispatcher that APPEARED was fine: this process was in the
		// judging branch, which returns each iteration, so the re-read was
		// reached. A dispatcher that VANISHED was not: this process was
		// inside AwaitRecord, nothing would ever settle the attempt
		// because the only thing that would have has died, and the re-read
		// was never reached again.
		//
		// Measured in the field: a dispatcher killed mid-build, a guest
		// that finished the build successfully, a record left saying
		// "building", and `bump --timeout 90m` sitting silent for the full
		// ninety minutes on a build that had passed.
		//
		// Asked FIRST, the loop re-decides its role on every pass whatever
		// the previous branch did, and both directions work for the same
		// reason.
		if reread != nil {
			res = reread(ctx)
		}
		switch res.State {
		case NoDispatcher:
			var err error
			if a, err = run.Finish(ctx, st, l, prov, local, a, spec, nil, by, now); err != nil {
				return a, err
			}
			// PACED, because nothing else paces it. The watcher branch below
			// sits inside AwaitFor for up to residencyRecheck; this branch
			// returns the moment Finish has looked, and Finish does not sleep.
			// So the judging role spun: a `tart list` and a `tart exec` into
			// the guest agent, as fast as the host would fork them, for the
			// whole of a multi-hour build.
			//
			// Measured on a cohort of five C++ ports: the guest's VM helper
			// trapped inside Apple's framework on an XPC event-handler thread,
			// three times, four to seven minutes in. `tart exec` reaches the
			// guest agent over that same channel. Hammering it is waste at
			// best and a cause at worst, and pacing is what makes the
			// difference measurable either way.
			if a.Phase != record.Finished {
				select {
				case <-ctx.Done():
				case <-time.After(judgeEvery):
				}
			}
		case ResidencyUnknown, DispatcherResident:
			var err error
			// BOUNDED, so a dispatcher that dies while this call is blocked
			// costs one interval rather than the whole --timeout.
			// AwaitRecord returns the attempt unchanged when the window
			// passes with no verdict, and the loop's own condition decides
			// what next.
			if a, err = run.AwaitFor(ctx, st, a.ID, 5*time.Second, residencyRecheck); err != nil {
				return a, err
			}
		}
	}
	if a.Phase == record.Finished || parent.Err() != nil {
		return a, nil
	}
	return reap(parent, st, l, prov, local, a, spec, by, now)
}

// reap stops a build whose caller's --timeout passed and settles the
// attempt against it. It is the whole of what a deadline does, and it is
// here rather than in cli because Change, Verify and Accept all wait
// through this one loop and a deadline must mean the same thing in all
// three.
//
// THE ORDER IS STOP THEN FINISH, and it is the whole reason Stop exists
// as its own capability. Finish's first act is Observe, which reads the
// guest's log; stopping first means the log it reads is the log of a
// build that has ended, so what a person opens afterwards is where the
// work actually got to rather than a snapshot from the middle of a race.
// Then the judge sees record.InterruptTimeout and KEEPS the environment,
// which is the point of stopping the work by signal instead of by
// Release: the guest outlives the build.
//
// A PROVIDER THAT CANNOT STOP IS SAID SO AND NOT PAPERED OVER (rule 7).
// The attempt still settles — the caller asked to stop waiting and that
// much is always deliverable — but the detail says the build is still
// running, because it is, and a person who reads "timed out" and finds a
// live build in `dockhand log` has been told a false thing by their own
// tool.
func reap(ctx context.Context, st *statestore.Store, l *ledger.Ledger, prov verify.Verifier, local run.Local, a record.Attempt, spec run.Spec, by lease.Claimant, now func() time.Time) (record.Attempt, error) {
	detail := "the --timeout passed; the build was stopped and its environment kept"
	switch err := run.Stop(ctx, st, prov, a); {
	case err == nil, errors.Is(err, verify.ErrUnknownJob):
		// Nothing there to stop is a build that is not running, which is
		// what was asked for.
	case errors.Is(err, run.ErrCannotStop):
		detail = "the --timeout passed; this provider cannot stop a build, so it is still running"
	default:
		detail = "the --timeout passed; the build could not be stopped and may still be running: " + err.Error()
	}
	itr := &record.Interrupt{Why: record.InterruptTimeout, By: by.Owner, At: now(), Detail: detail}
	return run.Finish(ctx, st, l, prov, local, a, spec, itr, by, now)
}

// waiting is the sentence a caller that stays prints before it does.
//
// It names the WORK, the SCALE and the WAY OUT, in that order, because
// those are the three things a person about to sit through a build needs
// and none of them is knowable from a blinking cursor. The way out is
// last and is the load-bearing half: Ctrl-C here costs nothing — the
// guest is in its own session, the branch is minted, the attempt is
// durable — and a person who does not know that will either wait for
// something they did not want or kill something they did.
func waiting(spec run.Spec, wait time.Duration) string {
	what := "the build"
	if n := len(spec.Roster); n > 1 {
		what = fmt.Sprintf("the cohort's %d builds", n)
	}
	how := "with no deadline"
	if wait > 0 {
		how = "for up to " + wait.String() + ", then stopping it"
	}
	return fmt.Sprintf("waiting on %s %s — a `port build` takes minutes to hours; Ctrl-C is safe and leaves it running, and `dockhand status` has the verdict either way", what, how)
}

// residencyRecheck is how long a watcher will sit inside AwaitRecord
// before re-deciding whose job the verdict is.
//
// It is short because the cost of being wrong is the whole --timeout: a
// watcher waiting on a dispatcher that has died learns nothing until it
// looks again, and looking is one lock probe. It is not shorter because
// the probe touches a lockfile and a store read, and a watcher is
// already polling the record every five seconds underneath.
const residencyRecheck = 30 * time.Second

// judgeEvery is how long the judging watcher waits before polling the
// provider again.
//
// It is the interval AwaitFor already gives the WATCHING role, chosen
// here for the same reason it was chosen there: the cost of being late
// is one interval, and a verdict somebody is waiting on is worth
// looking for often. What it must not be is absent, which is what it
// was — run.Finish does not pace itself, so this loop's only speed
// limit was how fast the host could fork `tart`.
const judgeEvery = 5 * time.Second

// Promote is the operation behind the promote verb, and the tail of
// bump --to-pr on a verifier-less host, sequenced by cli. Human BY
// CONSTRUCTION: Grants.Invoker is record.Human as a constant of the
// road, and PromoteIsHumanError deletes. Four stages — resolve, facts,
// authorize, apply — and it cannot do them in another order because
// publish.Apply takes a Permit only publish.Authorize returns.
//
// It NEVER observes, judges or settles runs: an Active attempt on the
// tip is Permit.Running, an advisory naming `dockhand cancel`; a
// finished-but-unsettled build is a remedy line by residency. The change
// record is NOT advanced here — a change closes ChangePublished only
// when the forge says merged, written by Cycle's close stage. A person
// is never paced: Authorize is handed a zero Pace and does not ask.
type Promote struct {
	Repo      *git.Repo
	Ledger    *ledger.Ledger
	State     *statestore.Store
	Env       publish.Env
	Grants    Grants
	Residency Residency
	Now       func() time.Time
	Progress  progress.Sink
}

// Grants is what this invocation is permitted to do. Invoker is a
// per-operation CONSTANT set by the road — Human on Promote and cycle,
// Machine on dispatch — and never an ambient value; the zero Driver is
// unset and is a wiring gap, never a person.
type Grants struct {
	Invoker record.Driver
	Grant   publish.Grant
}

// PromoteResult is what a promotion did. Published and Advisories live
// here and not on Result (the --to-pr delegation). Body is the preview
// under --body: emit after facts, do nothing else.
type PromoteResult struct {
	Published  *publish.Outcome
	Advisories []publish.Advisory
	Running    []string
	Body       string
}

// Run resolves, gathers, authorizes and applies. The invoker handed to
// Gather and the pace handed to Authorize are CONSTANTS of this road and
// not fields read off Grants: a promote is a person's act by
// construction, and a person is never paced.
func (p Promote) Run(ctx context.Context, target string, a publish.Asks) (PromoteResult, error) {
	ref, err := change.Resolve(ctx, p.Repo, p.State, target)
	if err != nil {
		return PromoteResult{}, err
	}
	f, err := publish.Gather(ctx, p.Env, ref, publish.ForgeRefresh, a, record.Human, p.Now())
	if err != nil {
		return PromoteResult{}, err
	}
	if a.Body {
		return PromoteResult{Body: f.Body}, nil
	}
	permit, adv, err := publish.Authorize(f, publish.Pace{})
	if err != nil {
		return PromoteResult{Advisories: adv}, err
	}
	out, err := publish.Apply(ctx, p.Env, permit)
	// the note, after the publication's own writes: record.Record carries
	// a Publication section and project() fills it, so a promotion that
	// never exported left a note that could not name a pull request under
	// any circumstances.
	exportNote(ctx, p.State, p.Ledger, ref.Tip(), p.Progress)
	return PromoteResult{Published: &out, Advisories: adv, Running: permit.Running()}, err
}

// The helpers below are named so the bodies above read as the sequence
// they claim. The three provider classifiers are the ONE place a
// provider sentinel is read in app: everything after them is a typed
// Deferral or a Realization.

// newChangeID mints a change's durable id, on run.EnqueueIn's precedent:
// random rather than derived, hex and sixteen bytes, because the id is a
// document name in a flat tree and a derived one would address a
// previous change of the same port at the same tip.
func newChangeID() record.ChangeID {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return record.ChangeID("chg-" + hex.EncodeToString(b[:]))
}

// branchFor is the branch a slug is minted under. It is git's naming and
// not app's — the namespace is spelled in exactly one place — and it
// takes the slug rather than the Prepared because a Prepared does not
// carry one: see ChangeRequest.Slug.
func branchFor(slug string) string { return git.MintBranchName(slug) }

// destination is the flags-to-record translation, and the only one. The
// three are the ladder record.Destination documents: --to-pr is bound
// ToPublished so the machine slot can see it, --no-verify stops at the
// branch, and the default asks for a verdict and stops there. A Document
// or InPlace road never reaches here, because neither mints.
//
// EVERY DELIVERY BUT --to-pr USED TO BE ToBranch, which made ToBranch
// disagree with its own doc — it says "--no-verify", and it was also
// every ordinary bump. Nothing branched on the difference, so nothing
// broke; what broke was a reader. publish.unrunCause took ToBranch at
// its word and told reviewers a branch had been minted with a flag
// nobody typed.
func destination(d Delivery) record.Destination {
	switch d {
	case PullRequest:
		return record.ToPublished
	case Branch:
		return record.ToBranch
	case Document, InPlace, Enqueue:
	}
	return record.ToVerdict
}

// attempts is every attempt in one read, in a stable order, so that
// Adoptable asked twice over one state answers with the same match.
func attempts(s statestore.State) []record.Attempt {
	out := make([]record.Attempt, 0, len(s.Attempts))
	for _, key := range slices.Sorted(maps.Keys(s.Attempts)) {
		out = append(out, s.Attempts[key])
	}
	return out
}

// rosterOf is the roster an ENQUEUE computes, and it must agree with
// run.Roster over the record the same Amend just wrote, because
// run.Spec.ID covers the roster by identity: a spec enqueued with an
// empty roster and re-derived at drain with a full one would hash to two
// different ids, and Adoptable would never match a bump's own attempt.
// For a freshly minted change there is no Accepted cohort finding and no
// attempt with runs, so Roster's answer is exactly this — every subject
// seated, in the record's order, with its Names carried across.
func rosterOf(subjects []record.Subject) []run.Member {
	out := make([]run.Member, 0, len(subjects))
	for _, s := range subjects {
		out = append(out, run.Member{Port: s.Port, Names: append([]string(nil), s.Names...)})
	}
	return out
}

// fromSourceOf names the subjects whose BINARY ARCHIVE MUST BE IGNORED,
// which is run.Spec.FromSource — and this function is the producer that
// field never had.
//
// The whole road below it was built and none of it was ever fed: the
// spec hashes FromSource, the frozen roster carries it, run.Plan
// intersects it with the ports being built, verify.Request declares it,
// tart reads it per member to pass `port -s`, and record.Ask.FromSource
// is what the pull request body and the ABI sentence read to say
// "built from source". Every one of those was exercised by tests
// handing the value in; nothing in the tree ever produced one, so
// `refresh-checksums` verified its re-derived checksums against the
// binary archive of the bytes it had just replaced. That is the one
// verification the flag exists to prevent.
//
// THE RULE IS THE REQUEST'S OWN, quoted: "A version bump does not need
// this: the new version yields an archive name that does not exist yet,
// so MacPorts builds from source on its own. A re-derivation at an
// unchanged version does, because the archive that matches predates the
// change and verifying against it would verify nothing."
//
// So it turns on the SUBJECT's intent and not on the change's, because
// a cohort's headline may be a re-derivation while its dependents are
// untouched ports that should build from their archives in seconds —
// which is exactly the distinction tart's own fromSource asks per
// member.
func fromSourceOf(subjects []record.Subject) []string {
	var out []string
	for _, s := range subjects {
		if s.Intent == IntentRefresh {
			out = append(out, s.Port)
		}
	}
	return out
}

// IntentRefresh is the one intent whose change leaves the VERSION where
// it was, and therefore the one whose verification must ignore the
// binary archive: the archive that matches predates the change.
//
// It is spelled here rather than imported because internal/intent is the
// CLI's catalogue and app must not depend on it. Exported so the tie can
// be a test rather than a hope: cli's intent_test.go holds this string
// against the catalogue's own Definition.Name, because a rename that
// only moved one of the two would silently stop refresh-checksums
// building from source and nothing would fail.
const IntentRefresh = "refresh-checksums"

// leaseOf is the lease token an attempt names, empty when nothing has
// started it. See Result.Lease for why the token and not the provider's
// own LeaseID.
func leaseOf(a record.Attempt) string { return a.Lease }

// verdictOf is the ONE verdict a settled attempt reports, over the
// per-member runs it carries, and the precedence is written out because
// this is where a cohort becomes an exit code.
//
// The order is worst-first in the sense the exit table means: a member
// that FAILED disproves the change and outranks everything; a member the
// environment could not answer for (errored, canceled, superseded) is
// louder than one that was merely never reached, because "we do not
// know" must not be reported as "blocked behind a sibling"; unsupported
// outranks blocked for the same reason. A settled attempt with no runs
// at all is Errored rather than Passed — nothing was measured, and rule
// 7 forbids reading an empty map as a pass.
// resumed is what an ADOPTED attempt earns instead of a start, and the
// answer for the two thirds of the adoptable population that cannot be
// started at all.
//
// run.Adoptable draws from three states — Queued, Active and settled
// Passed — and every caller in this package handed its answer straight
// to run.Start, which refuses anything but Queued (ErrNotQueued). So the
// two cases adoption exists FOR were the two that failed: `bump` or
// `verify` over a tip somebody had already verified came back as a mint
// error naming a branch that was perfectly fine, and one over a tip
// still building did the same. The road that pays nothing was the road
// that broke.
//
// Queued is the only startable answer, and the caller starts it. An
// ACTIVE adoptee is already running and is joined by watching, which is
// what --timeout does with any started attempt. A SETTLED one has its
// verdict earned: nothing to start, nothing to wait for, and Stood is
// the honest realization even without --timeout, because the answer the
// caller asked for is on the record already.
//
// A trace is the one thing adoption cannot give back, and the caller
// says so: Trace is outside the SpecID precisely so a --trace rerun
// still matches, and a build whose log is already closed is `dockhand
// log`'s.
func resumed(a record.Attempt) (Realization, bool) {
	switch {
	case a.Queued():
		return Queued, false
	case a.Active():
		return Started, true
	case a.Settled():
		return Stood, true
	}
	return Queued, false
}

func verdictOf(a record.Attempt) record.RunState {
	rank := func(s record.RunState) int {
		switch s {
		case record.Failed:
			return 5
		case record.Errored, record.Faulted, record.Canceled, record.Superseded:
			return 4
		case record.Unsupported:
			return 3
		case record.Blocked:
			return 2
		case record.Passed:
			return 1
		case record.Withheld:
			return 0 // a member kept out of the roster says nothing about the change
		case record.Queued, record.Submitting, record.Running:
			return 4 // a settled attempt holding an unfinished run is an incident
		}
		return 4
	}
	worst, seen := record.Errored, false
	for _, port := range slices.Sorted(maps.Keys(a.Runs)) {
		r := a.Runs[port]
		if rank(r.State) == 0 {
			continue
		}
		if !seen || rank(r.State) > rank(worst) {
			worst, seen = r.State, true
		}
	}
	if !seen {
		return record.Errored
	}
	return worst
}

// publicationOpen reports a publication of this change the forge has not
// settled. It is pure over the state the closure was handed, which is
// what lets supersedeIn choose between SupersedeIn and CloseIn without
// asking a forge from inside an Amend.
func publicationOpen(s statestore.State, id record.ChangeID) bool {
	for _, key := range slices.Sorted(maps.Keys(s.Publications)) {
		p := s.Publications[key]
		if p.Change == id && !p.Outcome.Settled() {
			return true
		}
	}
	return false
}

// exportNote writes the derived verify note for one commit, and it is
// the answer to statestore.Export's own question — "who calls it, since
// a derived view nobody derives is just an absent one". THE OPERATION
// THAT CHANGED A COMMIT-BOUND FACT CALLS IT, immediately after its
// Amend: a mint (so a `--no-verify` branch carries the record a reviewer
// reads with `git log --notes`), a settle, an extension, a publication
// (so a note can name a pull request at all), and Cycle's own re-export
// tail behind all of them.
//
// IT IS DEFERRED AT THE ROAD'S END rather than called at each Amend,
// because one operation may amend three times — mint, then enqueue, then
// settle under --timeout — and the note is a PROJECTION of the state as it
// finally stands, not a diary of the writes that got there. One export
// per road, over the state the road left.
//
// IT NEVER FAILS AN OPERATION AND NEVER SWALLOWS ANYTHING. Nothing reads
// a note to decide, so a note that could not be written cannot make a
// decision wrong and must not turn a landed mint into an error; and a
// note that was not written is exactly the kind of silence rule 7
// forbids, so it is SAID. Cycle's unconditional re-export is the backstop
// underneath both.
//
// IT MUST NOT BE CALLED FROM INSIDE AN AMEND CLOSURE: Export takes the
// store's lock, the flock is not reentrant, and a nested take is a writer
// waiting out its own deadline against itself.
func exportNote(ctx context.Context, st *statestore.Store, l *ledger.Ledger, sha string, p progress.Sink) {
	if st == nil || l == nil || sha == "" {
		return
	}
	if err := st.Export(ctx, l, sha); err != nil {
		say(p, progress.Warn, "the verify note on "+git.Abbrev(sha)+" was not written: "+err.Error())
	}
}

// readBeforeMint is the read a MINT road makes to ask what is already
// standing, and it is the one read in app that swallows
// statestore.ErrNoState.
//
// The rule is the sentinel's own: "only an explicit first write may
// swallow this. Any pass that destroys a provider resource refuses on it
// and says it cannot account for anything on this machine." A mint is
// exactly that explicit first write — the Amend below creates the ref —
// and the question this read answers is "does a change already stand for
// this port", whose honest answer on a checkout dockhand has never run
// in is no. Cancel and Discard destroy provider resources and Cycle
// deletes, so each of those propagates and says on a virgin checkout
// that it cannot account for anything here.
//
// STATUS IS THE THIRD CASE and it is neither of these: it writes no
// first record and destroys nothing, so it reads through
// statestore.ReadOrEmpty and reports the empty lifecycle. That is not a
// third rule — it is the sentinel's own rule read from the other side.
// This doc claimed the opposite until the field showed the cost: the
// first command after a purge, which removes the state ref by design,
// exited non-zero with "no state ref in this repository" where the
// honest answer was "nothing is in flight".
func readBeforeMint(ctx context.Context, st *statestore.Store) (statestore.State, error) {
	s, err := st.Read(ctx)
	if errors.Is(err, statestore.ErrNoState) {
		return statestore.State{}, nil
	}
	return s, err
}

// provider asks the composition root for a verifier. A nil function is
// "this road wired none", which is verify.ErrNoProvider and not a panic:
// see Change.Verifier for why that is a fact rather than a wiring gap.
func provider(ctx context.Context, fn func(context.Context) (verify.Verifier, error)) (verify.Verifier, error) {
	if fn == nil {
		return nil, verify.ErrNoProvider
	}
	return fn(ctx)
}

// say narrates through a sink that may be nil, because every operation's
// Progress is optional and a JSON caller wires none. It is the one place
// a nil sink is tolerated, so no body below has to test for one.
func say(p progress.Sink, l progress.Level, text string) {
	if p == nil {
		return
	}
	p.Say(l, text)
}

func isNoVacancy(err error) bool     { return errors.Is(err, verify.ErrNoVacancy) }
func isNoEnvironment(err error) bool { return errors.Is(err, verify.ErrNoEnvironment) }
func isNoProvider(err error) bool    { return errors.Is(err, verify.ErrNoProvider) }
