package app

import (
	"context"
	"errors"
	"maps"
	"path"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/planning"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Cycle is the pass: one run over everything durable this checkout
// already owns. `cycle` runs it once as a person (R3); `dispatch` runs
// it in a loop as the machine (R2), with the loop, the residency lock
// and the signals in cli — dispatch is not an operation and owns no
// lifecycle.
//
// IT DOES NOT SWEEP, PLAN, PREPARE, MINT OR ADMIT. Survey is the
// producer road and the ONLY caller of run.Admit; a pass that admitted
// would be capping a queue whose producer is itself, which is not a
// bound. A draft of this file had an admit stage and a proposed() set
// derived from "minted-and-unattempted" changes — a population that no
// longer exists, because every mint enqueues in the same Amend. It DOES
// hold a planner, for one stage only: the machine publish slot judges
// simplicity by change.Reconstruct, which takes the *plan.Plan only
// planning.Planner produces, and a draft that gave Cycle an evaluator
// and no planner had a dispatcher that "publishes by default" and could
// publish nothing, since every Reconstruction was Unjudged. The drain
// still re-STAGES a queued attempt from its sha and never re-plans.
//
// It is the only operation that touches all four durable lifecycles,
// so it is the one place rule 4 is load-bearing: every stage below calls
// a mutator EXPORTED BY THE PACKAGE THAT OWNS THAT KIND, and Cycle holds
// a *statestore.Store without ever reaching for a Txn.Put of its own.
//
// THE PASS LOCK AND THE RESIDENCY LOCK ARE THE CALLER'S. This operation
// opens no file: `cycle` try-locks .dockhand-pass.lock around one call
// and `dispatch` holds .dockhand-dispatch.lock for its whole life, both
// in cli (R2), so a pass that ran twice at once is a composition
// mistake and never a race this body could have prevented.
type Cycle struct {
	Repo   *git.Repo
	State  *statestore.Store
	Ledger *ledger.Ledger
	Env    publish.Env
	Grants Grants
	// Plan is the machine publish slot's planner (B3), with the fetcher
	// behind it acquired once and held for the process's life. The cost
	// is one fetch per candidate per tick, which is an argument for
	// --refresh-every, or for remembering a Reconstruction per (change,
	// tip) in process memory; both are cli's, and neither is this
	// sketch's.
	Plan planning.Planner
	// Eval is change.Reconstruct's evaluator, and it is a SECOND field
	// where the sketch reached into Plan.Eval with a type assertion. The
	// two evaluator seams in this tree are different method sets —
	// port.Oracle answers five questions about a target and
	// change.Evaluator answers one about a portdir — so
	// `Plan.Eval.(change.Evaluator)` would compile and panic on the
	// production evaluator. A nil Eval is not a failure: Reconstruct
	// answers ErrUnreconstructable, change.Judge reads that as Unjudged,
	// and Unjudged withholds, which is exactly what a machine road with
	// no way to re-plan should do.
	Eval change.Evaluator
	// Stage is the re-plan seam the drain ruling chose: a queued attempt
	// is materialized from its Sha at start, and a resident dispatcher
	// acquires the evaluator and fetcher behind it once and holds them.
	Stage run.Stager
	// Local is run.Finish's propose seam — the reverse index and the
	// Portfile cues — carried because the settle stage proposes.
	Local run.Local
	// Verifier is a function where the rest are values: a machine with no
	// tart still has a pass worth running — it settles, retires, publishes
	// and reports, and says once that nothing could be started.
	Verifier func(context.Context) (verify.Verifier, error)
	// Pace is the machine's allowance (R5), a value; Human passes carry
	// a zero one and never reach Authorize.
	Pace publish.Pace
	// Me is who this pass is. Since is the PROCESS start time and is
	// never restamped; the pass token is record.Claim.Pass, stamped from
	// the pass lockfile through Claimant.
	Me record.OwnerID
	// PassID is this pass's token off the pass lockfile, written onto
	// every claim this pass takes as record.Claim.Pass.
	PassID   string
	Now      func() time.Time
	Progress progress.Sink
}

// CycleRequest is what this invocation permits the pass to do. Every
// field is a policy or a permission and none of them is a fact. Admission
// is GONE from here: the pass admits nothing.
type CycleRequest struct {
	// Discharge is ONE knob over every obligation kind, defaulting ON,
	// spelled --no-discharge. DischargeAfter is the grace cutoff (F2): a
	// reconciler at the start of a five-minute pass meets work that is
	// seconds old, so an obligation younger than this is left alone.
	// ReclaimUnattributed is the person's `cycle --once` flag that lets a
	// pass seize untracked guests with NO attribution; cli refuses it on
	// a loop, exactly as it refuses --superseded.
	Discharge           bool
	DischargeAfter      time.Duration
	ReclaimUnattributed bool

	// Forge is publish.ForgePolicy now; cycle is ForgeRefresh, status is
	// ForgeAsCached, and a person's cycle cannot be told otherwise.
	Forge publish.ForgePolicy

	Retirement Retirement

	// Compact is `cycle --compact`, nil for every other pass. Its
	// MachineWindow is stamped here from publish.MaxWindow, never by a
	// flag.
	Compact *statestore.Retention

	// Publish walks the machine's publication slot, honoured only under
	// Grants.Invoker == record.Machine — a property of the road.
	Publish bool

	// Superseded deletes branches a newer sibling replaced. Opt-in.
	Superseded bool

	// DryRun withholds exactly the irreversible stages — discharge,
	// retire's deletions, apply, and compact — and performs every other
	// one, settling included.
	DryRun bool
}

// Retirement is what this pass may do about a change whose pull request
// the forge has finished with. The outcome row and the change's close
// are written under every value here (publish.RetireIn + change.CloseIn
// + run.WithdrawIn in one Amend); only the DELETION is a policy, and
// the deletion is change.DemolishIn's line in the retire Amend, under
// the machine road's refusals, plus the DeleteFork step for the fork
// copy.
type Retirement uint8

const (
	RetirementUnset  Retirement = iota
	ReportOnly                  // close both rows, delete nothing, name the verb that would
	Demolish                    // a merged change dockhand minted loses the branch dockhand made, through DemolishIn in the retire transaction, and its fork copy through the DeleteFork step
	WithholdDeletion            // --keep-merged: kept, and SAID to be kept
)

// Refusal is one change the pass could not carry forward, typed and
// band-bearing: what Attention walks. Edge-triggered by the
// dispatcher's own announced-at memory in cli, not by a record.
type Refusal struct {
	Change record.ChangeID
	Err    error
}

// Pass is what a cycle did: N outcomes, one row per change, and the
// report of what it could not do. Withheld and Counted are GONE (they
// are Sweep's); Vacancy stays as what the provider said, with its AsOf,
// for the report and never for a gate. Owed is lease's own value, with
// each obligation's Standing, so the report can name a foreign root.
type Pass struct {
	Changes    map[record.ChangeID]Result
	Refusals   []Refusal
	Ineligible []run.NotStarted
	Retired    []publish.Outcome
	// Published is every publish.Apply this pass made, the failed ones
	// included: see the publish stage for why the outcome of a failure is
	// kept, and Publications for the count a report may print.
	Published  []publish.Outcome
	Owed       []lease.Obligation
	Advisories []publish.Advisory
	Vacancy    verify.Vacancy
	Started    time.Time
	Ended      time.Time
	Compacted  *int
	// Maintained is whether `git maintenance run --auto` ran at the end of
	// this pass, and MaintainErr is why it did not. Two fields because the
	// zero value of one could not tell "not run" from "ran and said
	// nothing" (rule 7): a dry run performs no maintenance and has no
	// error, and a gc.lock held by the operator's own `git maintenance
	// start` is somebody else doing it — neither is a failed pass, and
	// neither is an exit band.
	Maintained  bool
	MaintainErr error
}

// Publications is what this pass PUT IN FRONT OF REVIEWERS, and it is
// deliberately not len(Published).
//
// The publish stage keeps publish.Apply's Outcome on the error path,
// because publish.Outcome.Completed is how a caller tells "pushed, no
// pull request" from "never left the machine". A report that counted
// every entry therefore printed a publication for a candidate whose
// Apply refused before any I/O at all — an `accept` between Gather and
// Apply returns ErrStale over a zero Outcome — and printed the row with
// an empty URL beneath it. Nothing was pushed, no pull request existed,
// and the pass said one was published.
//
// A publication is an entry that completed a step which OPENS OR MOVES a
// pull request. A push alone is not one: the branch is on the fork and
// the change is not in front of anybody, which is what the Refusal row
// beside it says. A no-op permit never reaches Apply, so it is not here
// to be counted either.
func (p Pass) Publications() []publish.Outcome {
	var out []publish.Outcome
	for _, o := range p.Published {
		for _, kind := range o.Completed {
			if kind == record.OpenPR || kind == record.RefreshPR {
				out = append(out, o)
				break
			}
		}
	}
	return out
}

// Attention is ruling 3's partition — an ALLOW-LIST of the quiet
// families, never a deny-list of the loud ones.
//
// A pass needs a person when it holds a REFUSAL, and the quiet families
// are named here rather than the loud ones so that an error type added
// next month is loud by default: the failure mode a deny-list has is
// that the new thing is silently quiet, and the failure mode this has is
// that a new thing is noisy until somebody argues it into the list.
//
// The two quiet families today: change.ErrNotBound, which is a peer
// closing a change first and is settled by the rerun that meets it, and
// ErrMachineMayNotDemolish, which is the machine declining to delete a
// person's branch — the person already said so with the hold.
func (p Pass) Attention() bool {
	for _, r := range p.Refusals {
		switch {
		case errors.Is(r.Err, change.ErrNotBound):
		case errors.Is(r.Err, ErrMachineMayNotDemolish):
		default:
			return true
		}
	}
	return false
}

// Exit is the pass's own code: 84 or 0. It is never Sweep's (F8).
func (p Pass) Exit() int {
	if p.Attention() {
		return 84
	}
	return 0
}

// Run performs the pass. THE STAGE GRID'S ROW ORDER IS NOT THIS ORDER: a
// pass runs different stages over different POPULATIONS — last pass's
// residue, this pass's live attempts, publications the forge finished
// with — and those have no order a column could express. Each edge
// below is an argument about what a crash leaves or what a slot costs:
//
//	discharge FIRST, over Standing's seize list older than
//	  DischargeAfter only, so obligations do not compete with this
//	  pass's starts for a slot.
//	settle BEFORE retire and drain: it frees slots and produces the
//	  verdicts publish and retire read.
//	retire (close) BEFORE drain: a close WITHDRAWS the change's queued
//	  attempts in the same Amend, so the drain never meets them — the
//	  reason a draft gave ("a merged change is not given a guest and
//	  then demolished") no longer holds once the drain walks attempts
//	  rather than branches, and this one does.
//	publish AFTER settle and BEFORE drain: publication frees no slot.
//	drain: run.Order, then run.Start until the FIRST ErrNoVacancy.
//	compact LAST but one, maintenance last — `git maintenance run
//	  --auto`, outside every lock, advisory on failure.
func (c Cycle) Run(ctx context.Context, r CycleRequest) (Pass, error) {
	p := Pass{Started: c.Now(), Changes: map[record.ChangeID]Result{}}
	prov, provErr := provider(ctx, c.Verifier)
	// closed is what this pass ENDED — retired, or swept as a stray — and
	// it exists for the re-export tail alone: a change that is no longer
	// Bound() is not in the tail's standing population, and the note on
	// its last commit is exactly the one that has just become wrong.
	closed := map[record.ChangeID]bool{}

	// 0 · DISCHARGE. Outstanding propagates statestore.ErrNoState rather
	// than an empty list, because this stage DESTROYS provider resources.
	// The seize list is lease.Standing's table under the Seizure policy;
	// everything else comes back with its Standing for the report.
	if r.Discharge && provErr == nil && !r.DryRun {
		obs, err := lease.Outstanding(ctx, c.State, prov, c.Me, c.PassID, c.Now())
		if err != nil {
			return p, err
		}
		pol := lease.Seizure{Set: true, Grace: r.DischargeAfter, Unattributed: r.ReclaimUnattributed}
		if p.Owed, err = lease.Discharge(ctx, c.State, prov, obs, pol, c.Claimant(), c.Now); err != nil {
			return p, err
		}
	}

	// 1 · SETTLE. run.Finish over every Active attempt this checkout
	// owns; Finish returns a still-running one unchanged, and proposes
	// the cohort for a passed one through Local.
	st, err := c.State.Read(ctx)
	if err != nil {
		return p, err
	}
	if provErr == nil {
		for _, a := range live(st, c.Me) {
			final, err := run.Finish(ctx, c.State, c.Ledger, prov, c.Local, a, specOf(st, a), nil, c.Claimant(), c.Now)
			if err != nil {
				return p, err
			}
			if final.Phase == record.Finished {
				p.Changes[a.Change] = Result{
					Did: Stood, Attempt: final.ID, Lease: leaseOf(final), Verdict: verdictOf(final),
				}
			}
		}
	}

	// 2 · CLOSE (retire). For each open publication: publish.Standing over
	// fresh ForgeFacts; when settled, the Cancel operation's stages first
	// (a merged change's live builds stopped, its KEPT leases released
	// through lease.Release — so a closed change never leaves a Held
	// lease behind), then ONE Amend with publish.RetireIn, change.CloseIn
	// (ChangePublished on merged, ChangeAbandoned on rejected or
	// withdrawn), run.WithdrawIn over its queued attempts and — under
	// Retirement Demolish, not DryRun, dem.Local != "", the branch
	// resolved (no ErrTipDisagrees), the worktree list read and the
	// branch not checked out, and mayDemolish re-asked INSIDE the closure
	// over tx.State() — change.DemolishIn, with publish.DeleteForkIn
	// beside it when dem.Fork != ""; then publish.DeleteFork over
	// publish.ForkOwed, outside every lock. Cancel's stages run over the
	// RECORD (id and tip) whenever a provider is present: a merged change
	// whose branch a hand moved or deleted still has live work to stop
	// and kept leases to release. A branch whose worktree list could not
	// be read, one a worktree has checked out, and one a hold or a follow
	// reached since the read, is closed WITHOUT its delete line and named
	// in Refusals: a foreign move stops a demolish, never a retirement.
	// The machine road never demolishes MintedVia Adopted and never past
	// a hold (change.Held ActDemolish); those refusals are Discard's and
	// this stage asks the same predicate.
	//
	// A CHANGE WHOSE REF A HAND MOVED IS NOT RETIRED THIS PASS, and that
	// is this port's one measured departure from the sketch's paragraph
	// above. The sketch closes such a change without its delete line;
	// doing so needs the forge's word, and in this tree the ONLY producer
	// of publish.ForgeFacts is publish.Gather, which takes a change.Ref —
	// whose only constructor is change.Resolve, which is exactly what a
	// moved ref refuses. A pass that observed the forge some other way
	// would be a second gatherer, which is the defect this package
	// exists to remove. So the disagreement is a Refusal row, the
	// publication stays open, and the person answers it with `verify` or
	// `discard`; nothing is deleted and nothing is lost.
	st, err = c.State.Read(ctx)
	if err != nil {
		return p, err
	}
	for _, key := range slices.Sorted(maps.Keys(st.Publications)) {
		pub := st.Publications[key]
		if pub.Outcome.Settled() {
			continue
		}
		old := st.Changes[string(pub.Change)]
		ref, resolveErr := change.Resolve(ctx, c.Repo, c.State, resolveTarget(old))
		if resolveErr != nil {
			p.Refusals = append(p.Refusals, Refusal{Change: old.ID, Err: resolveErr}) // moved by hand, or could not be read
			continue
		}
		f, err := publish.Gather(ctx, c.Env, ref, r.Forge, publish.Asks{}, c.Grants.Invoker, c.Now())
		if err != nil {
			p.Refusals = append(p.Refusals, Refusal{Change: old.ID, Err: err})
			continue
		}
		out, dem, err := publish.Standing(pub, f.Forge)
		if err != nil {
			// A FORGE THAT COULD NOT BE ASKED IS NOT A FORGE THAT SAID
			// "STILL OPEN", and collapsing the two into one `continue` is
			// the silence rule 7 forbids: publish.Standing refuses stale
			// facts (ErrNotFresh) and carries the lookup's own error, and a
			// pass that dropped both would report "0 retired · 0 refused"
			// for a host whose `gh` has been uninstalled for a week — the
			// absence of retirements as the operator's only signal, which is
			// the observable D13 exists to remove.
			p.Refusals = append(p.Refusals, Refusal{Change: old.ID, Err: err})
			continue
		}
		if !out.Settled() {
			continue // the forge answered, and the answer is that it is still open
		}
		if provErr == nil {
			// Cancel's stages over the RECORD: a moved or deleted branch
			// still owes the stop and the release of the recorded tip's work.
			cancel := Cancel{Repo: c.Repo, Ledger: c.Ledger, State: c.State, Verifier: c.Verifier, Local: c.Local, Me: c.Me, Now: c.Now, Progress: c.Progress}
			if _, err := cancel.run(ctx, st, old.ID, old.Tip); err != nil {
				return p, err
			}
		}
		wt, wtErr := "", error(nil)
		if r.Retirement == Demolish && !r.DryRun && dem.Local != "" {
			wt, wtErr = c.Repo.CheckedOutAt(ctx, old.Branch)
			switch {
			case wtErr != nil:
				p.Refusals = append(p.Refusals, Refusal{Change: old.ID, Err: wtErr}) // kept: could not read the worktree list (rule 7)
			case wt != "":
				p.Refusals = append(p.Refusals, Refusal{Change: old.ID, Err: change.ErrCheckedOut}) // kept: checked out
			}
		}
		demolish := r.Retirement == Demolish && !r.DryRun && dem.Local != "" && wtErr == nil && wt == ""
		fork := r.Retirement == Demolish && !r.DryRun && dem.Fork != ""
		to := record.ChangePublished
		if out != record.Merged {
			to = record.ChangeAbandoned
		}
		var kept error // the policy's refusal, re-asked under the flock; reset per closure run
		if err := c.State.Amend(ctx, func(tx *statestore.Txn) error {
			kept = nil
			// mayDemolish over the state THIS closure is handed: a `hold` or a
			// `verify` follow may have landed since the pass's read.
			may := mayDemolish(tx.State().Changes[string(pub.Change)], c.Grants.Invoker)
			if err := publish.RetireIn(tx, pub.ID, out, c.Now()); err != nil {
				return err
			}
			if fork && may {
				if err := publish.DeleteForkIn(tx, pub.ID, c.Now()); err != nil {
					return err
				}
			}
			if err := change.CloseIn(tx, pub.Change, to, "", c.Now()); err != nil {
				return err // change.ErrNotBound: a peer closed it first — a Refusal row, not a stop
			}
			run.WithdrawIn(tx, pub.Change, record.InterruptSuperseded, c.Me, c.Now())
			if (demolish || fork) && !may {
				kept = ErrMachineMayNotDemolish // held or adopted since the read: the close lands, the deletions do not
			}
			if demolish && may {
				return change.DemolishIn(tx, pub.Change, c.Now())
			}
			return nil
		}); err != nil {
			if errors.Is(err, change.ErrNotBound) {
				p.Refusals = append(p.Refusals, Refusal{Change: old.ID, Err: err})
				continue
			}
			return p, err
		}
		if kept != nil {
			p.Refusals = append(p.Refusals, Refusal{Change: old.ID, Err: kept})
		}
		closed[old.ID] = true
		p.Retired = append(p.Retired, publish.Outcome{Number: pub.Number, URL: pub.URL, At: c.Now()})
	}
	// the fork copies: a foreign effect, outside every lock, on its backoff.
	if !r.DryRun {
		st2, err := c.State.Read(ctx)
		if err != nil {
			return p, err
		}
		for _, pub := range publish.ForkOwed(st2) {
			if err := publish.DeleteFork(ctx, c.Env, pub, c.Now); err != nil {
				p.Refusals = append(p.Refusals, Refusal{Change: pub.Change, Err: err})
			}
		}
	}
	// The close stage also sweeps the change lifecycle's other deaths: a
	// branchless snapshot whose attempts are all settled closes Discarded
	// (CloseIn's own line drops the pin); --superseded demolishes the
	// local branch of a superseded change still carrying one. It never
	// rebinds, reverts or abandons: with the record and the ref in one
	// commit there is no half-written change for a pass to finish, and a
	// branch a person moved by hand is reported by status and answered by
	// the person. Written here as the body of strays.
	if err := c.strays(ctx, r, &p, closed); err != nil {
		return p, err
	}

	// 3 · PUBLISH, machine only, against the durable allowance. The
	// candidate population is every change with Destination ToPublished,
	// a Passed attempt on its tip, no hold a machine may not pass, and NO
	// publication row Open at the tip's Content (the already-in-front-of-
	// reviewers gate; Authorize's no-op permit is the backstop). Per
	// candidate: change.Reconstruct with Plan's plan against Base.Sha,
	// change.Judge for Simplicity; publish.Gather (ForgeRefresh, Spent
	// derived over the store); publish.Authorize against Pace — the first
	// ErrPaceSpent stops the slot; publish.Apply. The same three
	// functions Promote calls, with the invoker Machine.
	if r.Publish && c.Grants.Invoker == record.Machine && !r.DryRun {
		st3, err := c.State.Read(ctx)
		if err != nil {
			return p, err
		}
		for _, cand := range candidates(st3) {
			ref, err := change.Resolve(ctx, c.Repo, c.State, cand.Branch)
			if err != nil {
				p.Refusals = append(p.Refusals, Refusal{Change: cand.ID, Err: err})
				continue
			}
			pl, err := c.Plan.Plan(ctx, cand.Subjects[0].Intent, targetOf(cand.Subjects[0]), planParams(cand))
			if err != nil {
				p.Refusals = append(p.Refusals, Refusal{Change: cand.ID, Err: err})
				continue
			}
			rec, err := change.Reconstruct(ctx, c.Repo, cand, pl, c.Eval)
			if err != nil {
				p.Refusals = append(p.Refusals, Refusal{Change: cand.ID, Err: err})
				continue
			}
			simplicity, why := change.Judge(rec)
			f, err := publish.Gather(ctx, c.Env, ref, publish.ForgeRefresh, publish.Asks{}, record.Machine, c.Now())
			if err != nil {
				p.Refusals = append(p.Refusals, Refusal{Change: cand.ID, Err: err})
				continue
			}
			f.Simplicity, f.Why, f.Regions, f.Unattended = simplicity, why, rec.Regions, c.Grants.Grant
			permit, adv, err := publish.Authorize(f, c.Pace)
			p.Advisories = append(p.Advisories, adv...)
			if isPaceSpent(err) {
				break // the allowance is the durable count; nothing more this tick
			}
			if err != nil {
				p.Refusals = append(p.Refusals, Refusal{Change: cand.ID, Err: err})
				continue
			}
			if permit.NoOp() {
				continue // already in front of reviewers at this tip; not Spent
			}
			out, err := publish.Apply(ctx, c.Env, permit)
			if err != nil {
				p.Refusals = append(p.Refusals, Refusal{Change: cand.ID, Err: err})
			}
			// THE OUTCOME IS COLLECTED ON THE ERROR PATH TOO, and that is
			// deliberate: publish.Outcome.Completed exists precisely so a
			// caller can tell "pushed, no pull request" from "never left the
			// machine", and an Apply that pushed and then failed to open has
			// left a fact on the fork that a row of nothing would hide. What
			// this row is NOT is a publication, and that question is
			// Publications' — never len(Published).
			p.Published = append(p.Published, out)
		}
	}

	// 4 · DRAIN. A second read: the pass changed its own store above.
	if provErr == nil {
		st4, err := c.State.Read(ctx)
		if err != nil {
			return p, err
		}
		queued, ineligible := run.Pending(st4, c.Now())
		p.Ineligible = ineligible
		for _, a := range run.Order(queued, c.Now()) {
			started, err := run.Start(ctx, c.State, prov, c.Stage, a, c.Claimant(), c.Now())
			if err != nil {
				if isNoVacancy(err) {
					break // the sentinel is the authority; the rest stay Queued
				}
				continue // Defer wrote the backoff, or the deferral is reported on the row
			}
			p.Changes[a.Change] = Result{Did: Started, Attempt: started.ID, Lease: leaseOf(started)}
		}
		p.Vacancy = ask(ctx, prov) // reported, never consulted; asked AFTER the drain stopped
	}

	// 5 · COMPACT, only when asked, with the machine-row floor stamped
	// from the one constant; then maintenance.
	if r.Compact != nil && !r.DryRun {
		keep := *r.Compact
		keep.MachineWindow = publish.MaxWindow
		n, err := c.State.Compact(ctx, keep)
		if err != nil {
			return p, err
		}
		p.Compacted = &n
	}

	// 5b · RE-EXPORT, the backstop statestore.Export names: every note
	// dockhand writes comes through one function, and `cycle` rewrites the
	// ones whose projection is behind. UNCONDITIONAL, because it cannot
	// yet be conditional — record.Record carries no field for the state
	// commit a projection was read from, so there is no stamp to compare
	// and every candidate is rewritten. The population is the changes a
	// note can still be wrong about: the standing ones, plus the ones THIS
	// pass closed, whose notes went stale in the retire Amend above.
	//
	// AFTER COMPACT, deliberately: a change compacted away this pass has
	// no record to project, and Export answers ErrNoChangeAt for it, which
	// is the honest end of a note nothing can regenerate. Running before
	// compact would rewrite it one last time and leave the same permanent
	// staleness a discard removes its note to avoid.
	c.reexport(ctx, &p, closed)

	// 6 · MAINTAIN, unconditionally the last line and regardless of
	// whether compact ran — the robot pays its own housekeeping bill at a
	// moment nobody is waiting (Q09). Skipped under --dry-run, because a
	// repack is irreversible work even though it changes no state this
	// design records. Its failure is an advisory and never a band: `git
	// maintenance run --auto` refusing a gc.lock the operator's own
	// scheduled maintenance holds is somebody else doing it, and a pass
	// that settled, retired, published and drained did not fail because
	// of it. It is asked here rather than in cli because a second front
	// end driving this operation would otherwise inherit the debt (R2
	// gives cli the loop, the lock and the signals only).
	if !r.DryRun && c.Repo != nil {
		if err := c.Repo.Maintain(ctx); err != nil {
			p.MaintainErr = err
		} else {
			p.Maintained = true
		}
	}
	p.Ended = c.Now()
	return p, nil
}

// reexport rewrites the derived note on every commit whose projection
// this pass could have moved, and fails nothing: a read it could not
// make is a pass that says so and carries on, because a note is a view
// and no decision in this tree reads one.
//
// It is Cycle's alone. Every other road exports the ONE commit it
// touched, immediately after its own Amend; this is the sweep that
// catches the process that died between those two writes, and a process
// that dies is exactly the case the operation's own call cannot cover.
func (c Cycle) reexport(ctx context.Context, p *Pass, closed map[record.ChangeID]bool) {
	if c.Ledger == nil {
		return
	}
	st, err := c.State.Read(ctx)
	if err != nil {
		say(c.Progress, progress.Warn, "the verify notes were not re-exported: "+err.Error())
		return
	}
	for _, key := range slices.Sorted(maps.Keys(st.Changes)) {
		ch := st.Changes[key]
		if ch.Tip == "" || (!ch.Bound() && !closed[ch.ID]) {
			continue
		}
		exportNote(ctx, c.State, c.Ledger, ch.Tip, c.Progress)
	}
}

// strays is the close stage's sweep over the change lifecycle's other
// deaths, described above Run's call to it. Each case is one Amend of
// owned mutators; what it could not do is a Refusal row, never silence
// (rule 7). A refused Amend in either case is a Refusal row of THIS
// pass and never a stop. Under R23 supersedeIn demolishes the old local
// branch in-transaction on every replace and Survey supersede, so
// --superseded's population is a superseded change whose branch a hand
// recreated; retiring CycleRequest.Superseded is not settled here.
func (c Cycle) strays(ctx context.Context, r CycleRequest, p *Pass, closed map[record.ChangeID]bool) error {
	st, err := c.State.Read(ctx)
	if err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(st.Changes)) {
		ch := st.Changes[key]
		switch {
		case ch.Pin != "" && ch.Bound() && allSettled(st, ch.ID):
			// a snapshot nobody can extend: closed, and CloseIn's own line
			// drops the pin.
			if err := c.State.Amend(ctx, func(tx *statestore.Txn) error {
				return change.CloseIn(tx, ch.ID, record.ChangeDiscarded, "", c.Now())
			}); err != nil {
				p.Refusals = append(p.Refusals, Refusal{Change: ch.ID, Err: err}) // a hand-moved pin (45), or a peer's close
				continue
			}
			closed[ch.ID] = true
		case r.Superseded && !r.DryRun && ch.SupersededBy != "" && ch.Branch != "" && c.Repo.HasBranch(ctx, ch.Branch) && mayDemolish(ch, c.Grants.Invoker):
			wt, err := c.Repo.CheckedOutAt(ctx, ch.Branch)
			if err != nil {
				p.Refusals = append(p.Refusals, Refusal{Change: ch.ID, Err: err}) // could not read the worktree list
				continue
			}
			if wt != "" {
				p.Refusals = append(p.Refusals, Refusal{Change: ch.ID, Err: change.ErrCheckedOut})
				continue
			}
			if err := c.State.Amend(ctx, func(tx *statestore.Txn) error {
				if !mayDemolish(tx.State().Changes[string(ch.ID)], c.Grants.Invoker) {
					return ErrMachineMayNotDemolish // held or followed since the read
				}
				return change.DemolishIn(tx, ch.ID, c.Now())
			}); err != nil {
				p.Refusals = append(p.Refusals, Refusal{Change: ch.ID, Err: err}) // a hand-moved branch (45), the policy, or a peer
			}
		}
	}
	return nil
}

// ask obtains the provider's vacancy for the REPORT. A backend that does
// not implement it yields an unknown Vacancy, reported as unknown. It is
// asked after the drain has already stopped on the sentinel, so it can
// never become a gate by accident of ordering.
func ask(ctx context.Context, prov verify.Verifier) verify.Vacancy {
	r, ok := prov.(verify.VacancyReporter)
	if !ok {
		return verify.Vacancy{}
	}
	v, err := r.Vacancy(ctx)
	if err != nil {
		return verify.Vacancy{}
	}
	return v
}

// Claimant carries the pass token onto every claim this pass takes.
func (c Cycle) Claimant() lease.Claimant { return lease.Claimant{Owner: c.Me, Pass: c.PassID} }

// live is every Active attempt THIS CHECKOUT owns, in a stable order.
// The comparison is the owner's Root and not the whole OwnerID, on
// lease.standingOf's precedent: a checkout is a root, and an attempt
// this morning's process started is still this checkout's to settle.
func live(s statestore.State, me record.OwnerID) []record.Attempt {
	var out []record.Attempt
	for _, key := range slices.Sorted(maps.Keys(s.Attempts)) {
		if a := s.Attempts[key]; a.Active() && a.Owner.Root == me.Root {
			out = append(out, a)
		}
	}
	return out
}

// allSettled reports a change with no attempt left running or queued —
// the predicate the snapshot sweep closes on. A change with NO attempts
// at all is not settled by this: a snapshot minted a second ago has
// nothing to settle and closing it would race its own enqueue.
func allSettled(s statestore.State, id record.ChangeID) bool {
	any := false
	for _, key := range slices.Sorted(maps.Keys(s.Attempts)) {
		a := s.Attempts[key]
		if a.Change != id {
			continue
		}
		any = true
		if !a.Settled() {
			return false
		}
	}
	return any
}

// candidates is the machine publish slot's population, in a stable
// order: bound for publication, verified at the tip that stands, past no
// hold a machine may not pass, and not already in front of reviewers at
// this content.
//
// The last clause is the already-in-front-of-reviewers gate, and it is
// asked over the CONTENT rather than the tip: a rebase that changes no
// files is the same change to a reviewer, and a pass that re-published
// it would spend the allowance on a no-op. Authorize's no-op permit is
// the backstop underneath it.
func candidates(s statestore.State) []record.Change {
	var out []record.Change
	for _, key := range slices.Sorted(maps.Keys(s.Changes)) {
		c := s.Changes[key]
		switch {
		case !c.Bound(), c.Destination != record.ToPublished, c.Branch == "", len(c.Subjects) == 0:
			continue
		case change.Held(c, change.ActPublish, record.Machine) != nil:
			continue
		case !passedAtTip(s, c):
			continue
		case openAtContent(s, c):
			continue
		}
		out = append(out, c)
	}
	return out
}

// passedAtTip reports a settled attempt on the change's current tip
// whose verdict is a pass. It walks attempts rather than reading a field
// because there is no such field: a verdict is the attempt's, and a
// change that carried one would be a second place to read it from.
func passedAtTip(s statestore.State, c record.Change) bool {
	for _, a := range onTip(s, c.ID, c.Tip) {
		if a.Settled() && verdictOf(a) == record.Passed {
			return true
		}
	}
	return false
}

// openAtContent reports an unsettled publication of this change already
// standing at this content.
func openAtContent(s statestore.State, c record.Change) bool {
	for _, key := range slices.Sorted(maps.Keys(s.Publications)) {
		pub := s.Publications[key]
		if pub.Change == c.ID && !pub.Outcome.Settled() && pub.Content == c.Content {
			return true
		}
	}
	return false
}

// planParams is the parameters a RE-PLAN of an existing change is made
// with: the subject's own portdir as the target, and the target value
// the subject recorded — a version for a bump, "checksums" for a
// re-derivation — so that Reconstruct compares this change against the
// plan the same intent would make today.
//
// Riders are RidersNone on purpose. A reconstruction asks whether the
// change's edits are confined to the regions the machine may publish,
// and a re-plan that added a modeline the original did not have would
// answer no to a question about housekeeping rather than about the
// change.
func planParams(c record.Change) intent.Params {
	s := c.Subjects[0]
	return intent.Params{
		Target:  s.Portdir,
		Version: s.Target,
		Reason:  s.Reason,
		Riders:  intent.RidersNone,
	}
}

// targetOf is the resolved target a re-plan runs over: the subject's
// portdir, with the subport named when the record says the member is not
// the portdir's own top-level port. tree.Target is two strings and
// naming it here opens nothing — the precedent is run naming
// portindex.Dependent for the same reason.
func targetOf(s record.Subject) tree.Target {
	t := tree.Target{Portdir: s.Portdir}
	if s.Port != "" && s.Port != path.Base(s.Portdir) {
		t.Subport = s.Port
	}
	return t
}

func isPaceSpent(err error) bool { return errors.Is(err, publish.ErrPaceSpent) }
