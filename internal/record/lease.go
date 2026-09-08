package record

import (
	"time"

	"github.com/herbygillot/dockhand/internal/edit"
)

// LeaseID names one environment the provider handed out. It is
// declared here, and NOT taken from the verify package, because a
// persisted type must not be hostage to the shape of an interface:
// today record.JobRecord embeds verify.Job, so changing the provider
// contract changes the on-disk schema. Three fields are cheap; the
// adapter converts.
type LeaseID struct {
	Provider string    `json:"provider"`
	ID       string    `json:"id"`
	Started  time.Time `json:"started"`
}

// Lease is one environment this checkout is accountable for. The
// distinction that matters is between holding one and owing its
// return: today a single Released bool means "we claimed the right to
// hand it back" where one call site writes it and "the provider has it
// again" where three others read it.
type Lease struct {
	Schema int     `json:"schema"`
	ID     LeaseID `json:"id"`
	// Request is the token the CALLER minted and the provider echoed
	// back. It is what recovery joins on, and it is written before the
	// provider is called — which is the whole of rule 3.
	Request  string     `json:"request"`
	Handle   string     `json:"handle,omitempty"` // the provider's own name, once known
	Change   ChangeID   `json:"change"`
	Owner    OwnerID    `json:"owner"`
	Platform string     `json:"platform"`
	Phase    Phase      `json:"phase"`
	Test     bool       `json:"test,omitempty"`
	TreeAsOf time.Time  `json:"tree_as_of,omitzero"`
	Claim    *Claim     `json:"claim,omitempty"`
	Release  *Release   `json:"release,omitempty"`
	Retain   *time.Time `json:"retain,omitempty"` // a deadline, not a verdict
}

// Phase is how far an effect that can outlive this process has got. It
// exists because "we asked and do not know" is a state, and today it is
// spelled as the absence of a record.
type Phase string

const (
	// Requested is written BEFORE the provider is called.
	Requested Phase = "requested"
	Acquiring Phase = "acquiring"
	Active    Phase = "active"
	Finished  Phase = "finished"
	// Uncertain is the honest state after a call whose outcome was never
	// observed. A timeout does not prove a VM was not created.
	Uncertain Phase = "uncertain"
)

// OwnerID is who is accountable, and Root has ONE source rather than
// the two spellings D20 is about.
//
// It is called Root and not Repo because of a road the design nearly
// missed: `dockhand verify <portdir>` falls through to the synchronous
// gate PRECISELY when no repository or no branch resolved
// (internal/cmd/verify.go:35-49), so there are environment-taking
// invocations with no repository at all. That is why the gate submits
// with the TREE root today — it is the only identity available — and it
// is the real reason ownership had two spellings rather than mere
// sloppiness.
//
// Root is the WHOLE identity, and a draft of this design briefly added a
// bool beside it saying whether the root was a repository. That was
// wrong in the common case: a ports tree usually IS the repository
// checkout, so the same directory would carry two owners that compared
// unequal — inventing the divergence D20 is about rather than closing
// it. One value, resolved once: the repository root where there is one,
// and the tree root otherwise.
//
// ONE SOURCE IS NOT ONE SPELLING, and a reader was right to say so.
// Ownership is decided by string equality, and a path has many
// spellings for one place: /tmp against /private/tmp on macOS, a
// case-insensitive filesystem, a trailing slash, a symlinked home,
// `--tree ~/ports` against `--tree /Users/x/ports`. Any of those and
// this checkout's own leases read as foreign, which is D20 with extra
// steps — a foreign obligation is reported and never seized, so the
// environment is never released, the obligation is never Done, and the
// record is therefore never closed and never compacted. So Root is
// CANONICAL by construction: EvalSymlinks, then Abs, then Clean, at the
// single point that resolves it, and it is written nowhere else.
//
// Since is the process's start time, and it is here because PID alone
// cannot decide liveness: PIDs are reused, so a same-host recovery that
// checks only "is something alive at 4821" eventually finds an
// unrelated process and refuses the reclaim forever. The pair is what
// identifies a process; the number alone is a coincidence waiting to
// happen.
type OwnerID struct {
	Root  string    `json:"root"`
	Host  string    `json:"host"`
	PID   int       `json:"pid"`
	Since time.Time `json:"since"`
}

// SpecID identifies what a verification was ASKED FOR. It lives here
// beside ContentID because both are durable identities the record
// stores; run consumes it rather than declaring its own.
type SpecID string

// Attempt is one verification, whether or not a commit for it exists.
type Attempt struct {
	Schema int    `json:"schema"`
	ID     string `json:"id"`
	// Change is the attempt's subject, and it is the CHANGE and not the
	// sha because a pre-mint gate has no sha. It is also what the
	// attempt's lease slot is derived from, so an attempt that cannot
	// name its change cannot claim, discharge or hand back the
	// environment it is holding.
	Change  ChangeID  `json:"change"`
	Spec    SpecID    `json:"spec"`
	Content ContentID `json:"content"`
	// Sha is REQUIRED at enqueue and never empty. A draft had it "empty
	// before a mint" for the pre-mint gate, and the gate is deleted: every
	// road now commits BEFORE it enqueues (Change and Survey write an
	// unreferenced commit, Verify's working-tree road snapshots one), so
	// the queue carries a commit a drain can materialize — an identity and
	// a question, never a filesystem path. run.EnqueueIn refuses an empty
	// one rather than writing an attempt nothing could ever build.
	Sha string `json:"sha"`
	// Platform is on the attempt because the exported note's run map is
	// keyed by (port, platform) and the attempt is where both halves have
	// to come from. It cannot come from the lease: a QUEUED attempt has
	// no lease, and recovering it from the lease's slot name would mean
	// splitting a joined string, which is the defect RunKey exists to
	// remove.
	Platform string `json:"platform"`
	// Owner is who holds the attempt's live work — the process that
	// started it and will judge it. EnqueuedBy is who ASKED: the person's
	// shell that ran `bump`, or the sweep. They part the moment dispatch
	// is the process that starts what a person queued, and the store must
	// answer "who queued this" months later from the record alone (open
	// question 7, answered yes). Owner moves when a dispatcher takes over
	// a queued attempt; EnqueuedBy never does.
	Owner      OwnerID `json:"owner"`
	EnqueuedBy OwnerID `json:"enqueued_by"`
	// Ask is what the enqueuer asked of the build — --test, --keep-env —
	// carried on the ATTEMPT because a queued attempt has no Run yet, and
	// the drain that starts it hours later must honour the ask the person
	// made rather than the flags the dispatcher was launched with (the F4
	// shape, one field over). Run.Ask on each member is stamped from this
	// at start.
	Ask     Ask       `json:"ask,omitzero"`
	Started time.Time `json:"started"`
	Phase   Phase     `json:"phase"`
	Lease   string    `json:"lease,omitempty"`
	Members []string  `json:"members,omitempty"`
	// Interrupt is the observation that a person or a supersession stopped
	// this attempt before the provider answered. It is DURABLE and it is
	// EVIDENCE: run.Judge reads it (Evidence.Interrupt) and returns every
	// member Canceled or Superseded, so a cancellation is an observation the
	// one judge interprets rather than a second verdict-writer. Nil means
	// nobody interrupted, and that is the only thing nil means — an
	// interrupted attempt always carries one because Finish writes it in
	// the same Amend as the verdict.
	Interrupt *Interrupt `json:"interrupt,omitempty"`

	// NotBefore, Tries and LastError are the backoff state, and they are
	// the same three fields Release already carries for the identical
	// problem. Without them run.Order's stated ordering — NotBefore,
	// then age — reads a field that does not exist, and the second defect
	// Order claims to fix is unfixable: a submission that fails for its
	// own reasons has nowhere to record that it failed, so a nightly pass
	// re-submits a permanently broken port, boots a VM and fails again,
	// forever.
	NotBefore *time.Time `json:"not_before,omitempty"`
	Tries     int        `json:"tries,omitempty"`
	LastError string     `json:"last_error,omitempty"`

	// Runs are this attempt's per-member verdicts, and they live HERE
	// rather than only on the exported note. An earlier draft left them
	// on record.Record alone, which quietly made the note authoritative
	// for the one thing a verification exists to produce — contradicting
	// the ruling that the note is a derived export. Two independent
	// readers found it, and they were right: an attempt IS one
	// verification of N members, so its members' outcomes are part of it
	// and not a separate document living somewhere else.
	Runs map[string]Run `json:"runs,omitempty"` // keyed by member port
}

// Interrupt is why an attempt was stopped before the provider finished
// with it, and by whom. Two producers: `dockhand cancel` (and the
// operations that compose it — Discard, bump --replace) write Canceled;
// the stale stage on Verify, Accept and Cancel writes Superseded when a
// branch's tip moved past the attempt. The shipped tree told these apart
// by which sentence it wrote into Run.Detail ("canceled: the branch moved
// to ..." against "canceled by the user") and `status` read them back by
// prefix, which is rule 6's defect exactly. Why is typed so nothing reads
// words; Detail is what a person reads and nobody decides from.
type Interrupt struct {
	Why    InterruptWhy `json:"why"`
	By     OwnerID      `json:"by"`
	At     time.Time    `json:"at"`
	Detail string       `json:"detail,omitempty"`
}

// InterruptWhy is the typed cause. The zero value names none, so an
// Interrupt built and not filled in cannot read as a cancellation.
type InterruptWhy string

const (
	InterruptUnknown    InterruptWhy = ""
	InterruptCanceled   InterruptWhy = "canceled"   // a person asked
	InterruptSuperseded InterruptWhy = "superseded" // the tip moved past it
)

// Active reports an attempt that has an environment and no verdict yet:
// it is what the stale stage stops and what Promote reports as an
// advisory. It is neither Queued (no lease) nor Settled (finished), and
// it is a method rather than a phase test at four call sites so that a
// new Phase is a compile-time visit here.
func (a Attempt) Active() bool { return a.Lease != "" && a.Phase != Finished }

// Queued reports an attempt that exists and has not started. It is the
// counterpart to Settled, and run.Pending selects on it — the design
// declared a queue, an ordering over it and a cap on it before anything
// could say which attempts were IN it.
func (a Attempt) Queued() bool { return a.Phase == Requested && a.Lease == "" }

// Settled reports that an attempt is over, which is what Compact tests.
// A terminal phase and nothing owed: evidence is immutable after, and a
// retry is a new attempt rather than a reopening of this one.
func (a Attempt) Settled() bool { return a.Phase == Finished }

// Publication is keyed by the change, not by whichever commit was at
// the tip when a forge call was made.
type Publication struct {
	Schema int      `json:"schema"`
	ID     string   `json:"id"`
	Change ChangeID `json:"change"`
	// By and Basis are why this publication was permitted, recorded so a
	// person can answer "which of these did a machine open, and on what
	// grounds" months later, from the record alone.
	//
	// They are PROVENANCE and never an input to a gate. That discipline is
	// the tree's own, stated where AskedBy is written: a provenance field
	// "that could widen what the unattended road is allowed to do would be
	// an authorization rather than a record of what happened"
	// (internal/engine/mint.go:369-375). So publish.Authorize takes the
	// simplicity verdict as a PARAMETER, recomputed over the realized
	// change, and writes it here afterwards. A stored verdict read back as
	// permission is a forgeable token.
	By      Driver    `json:"by"`
	Basis   []Region  `json:"basis,omitempty"`
	Content ContentID `json:"content"`
	Target  string    `json:"target"`
	Steps   []Step    `json:"steps,omitempty"`
	Number  int       `json:"number,omitempty"`
	URL     string    `json:"url,omitempty"`
	Outcome Outcome   `json:"outcome,omitempty"`
}

// Outcome is how a publication ended. It is an enum and not a free
// string because the retire pass MATCHES ON IT to resolve a publication
// left uncertain between the push and the pull request, and D14 grades
// an unowned string vocabulary unrepresentable on exactly that ground:
// a writer that invents a spelling writes a row a later build cannot
// classify.
type Outcome string

const (
	Open      Outcome = "open"
	Merged    Outcome = "merged"
	Rejected  Outcome = "rejected"
	Withdrawn Outcome = "withdrawn"
)

// Settled reports a publication whose life is over — the predicate
// Compact tests, and the one that must be false for a change's death to
// be permitted, since a merged pull request outlives the branch that
// carried it.
func (o Outcome) Settled() bool {
	return o == Merged || o == Rejected || o == Withdrawn
}

// Region is one edit kind and how many edits of it a change made. It is
// the durable form of the basis for a machine publication: not the
// sentence "it was simple", but what the change was actually made of.
type Region struct {
	Kind  edit.Kind `json:"kind"`
	Edits int       `json:"edits"`
}

// Step is one effect a publication performed, recorded BEFORE it is
// attempted and completed after.
//
// Kind is an enum living HERE, and a draft of this design had it as a
// free string on this struct while publish declared a second, unrelated
// Step enum one import away. That is the same-name collision this whole
// document is about, committed twice inside it: the durable side was an
// unowned vocabulary and the typed side could not reach the records.
type Step struct {
	Kind    StepKind  `json:"kind"`
	Phase   Phase     `json:"phase"`
	At      time.Time `json:"at"`
	Detail  string    `json:"detail,omitempty"`
	Attempt int       `json:"attempt,omitempty"`
	// NotBefore is the backoff a refused foreign effect writes — the same
	// spelling Attempt and Release carry (a pointer: nil is "no backoff",
	// never a zero time doubling as one) — so a fork copy the remote will
	// not delete is not pushed at on every five-minute tick.
	// publish.ForkGoneIn writes it; publish.ForkOwed reports a step under
	// it as owed and publish.DeleteFork skips it until it passes.
	NotBefore *time.Time `json:"not_before,omitempty"`
}

// StepKind is the ordered set of effects a permit can authorize. It is
// the single declaration: publish names these rather than shadowing
// them.
type StepKind string

const (
	PushBranch    StepKind = "push-branch"
	OpenPR        StepKind = "open-pr"
	RefreshPR     StepKind = "refresh-pr"
	RecordOutcome StepKind = "record-outcome"
	// DeleteFork is retirement's one foreign effect: the push-delete of
	// the fork copy after the forge merged. It is a STEP on the
	// publication row because it is the one deletion in the design that
	// cannot join a local batch, so it keeps the shape every foreign
	// effect keeps — written Requested by publish.DeleteForkIn before the
	// push, Finished or Uncertain by publish.ForkGoneIn after — and a
	// pass that finds it unfinished retries it (publish.ForkOwed). The
	// local branch and the pin are NOT steps: they die in the closing
	// batch, and nothing records what a batch made unrepresentable.
	DeleteFork StepKind = "delete-fork"
)

// Claim is who took a slot and until when. The expiry is durable and
// not merely a field on the live claimant, because the pass that has to
// decide whether a claim is stale is a DIFFERENT process reading the
// record — one that can see neither the claimant's memory nor, across
// hosts, its process table.
type Claim struct {
	By      string    `json:"by"`
	At      time.Time `json:"at"`
	Expires time.Time `json:"expires"`
	// Pass is the PASS TOKEN: the id of the cycle pass that took this
	// claim, stamped from the pass lockfile. It is here and NOT on
	// OwnerID.Since, which a draft proposed restamping per pass (F3):
	// Since is the process's start time and half of the liveness pair
	// that decides whether a PID is the same process, so moving it per
	// pass would make a resident dispatcher's own leases read as a
	// stranger's between passes. The token is a REPORT field — it tells
	// a person which pass left a lease behind — and never a seize
	// condition, because discharge runs first in a pass and nothing of
	// the current pass can exist when it does (lease.Standing).
	Pass string `json:"pass,omitempty"`
}

// Release is an obligation, not a fact. Requested records that this
// checkout took responsibility for handing the environment back and
// no other pass should try; Done records that the provider confirmed
// it. Requested without Done is work owed, and it is exactly the state
// a crash between the two leaves behind.
type Release struct {
	Requested time.Time  `json:"requested"`
	By        string     `json:"by"`
	Done      *time.Time `json:"done,omitempty"`
	Attempts  int        `json:"attempts,omitempty"`
	LastError string     `json:"last_error,omitempty"`
	// NotBefore is the backoff a refused release writes — the same three
	// fields Attempt carries for the same problem — so a resident pass at
	// five-minute cadence does not retry a refusing provider every tick.
	// lease.Discharge writes it; lease.Outstanding reports an obligation
	// under it as standing and Discharge skips it until it passes.
	NotBefore *time.Time `json:"not_before,omitempty"`
}

// Held reports that this checkout still has the environment and owes
// nothing yet.
func (l Lease) Held() bool { return l.Release == nil }

// Owed reports an unfulfilled obligation: a release this checkout
// claimed and did not finish. The reconciler retries these, and it can
// only see them because the claim and the completion are two fields.
func (l Lease) Owed() bool { return l.Release != nil && l.Release.Done == nil }

// Returned reports that the provider confirmed the handback.
func (l Lease) Returned() bool { return l.Release != nil && l.Release.Done != nil }
