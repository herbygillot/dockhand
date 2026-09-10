package app

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lease"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/run"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Survey is THE PRODUCER ROAD: bump | bump-revision | refresh-checksums
// under a selector, and outdated / classify as its plan-only reports.
// It is the ONLY caller of run.Admit, and it is INCREMENTAL: targets
// arrive one at a time as the pool plans and prepares them, and each
// one is admitted, committed, recorded — record and branch in one batch
// — and opportunistically started before the next is looked at, with
// its row emitted as it lands. A draft admitted the whole selector at
// once after every target had prepared, and an adversarial pass priced
// it: a 400-port bump sweep fetches one target at a time, up to twenty
// minutes each, so nothing was committed for hours and a Ctrl-C lost all
// of it — where the shipped sweep mints per target as it arrives and
// resumes by rerun. Resume-by-rerun is InFlight Advance: a rerun meets
// its own standing branches and leaves them as Stood rows.
//
// Per target, ONE Amend: run.Admit over run.Count(tx.State()) and this
// sweep's own count — MaxQueued read under the flock every time, no
// in-process copy of it; MaxPerPass the sweep's unit — then, for a
// selector meeting its own standing branch, InFlight Advance leaves it
// (a Stood row, nothing written) while Supersede runs the supersede
// closure step FIRST (change.SupersedeIn while the old change's
// publication is open, else change.CloseIn(old, Superseded, by: new);
// run.WithdrawIn over its queued attempts); then change.MintIn; then
// run.EnqueueIn unless --no-verify. A withheld target has no record, no
// branch, no attempt — it is a row (run.NotAdmitted, typed Why), never
// dropped, and can never be confused with a --no-verify branch. Then
// change.Resolve for the row's Ref; the supersede stage over the old
// tip's live work ran BEFORE the Amend, in Change.Run's order, since the
// batch deletes (or re-points) the old branch; and run.Start, until the
// FIRST verify.ErrNoVacancy in this process — NEVER asking whether the
// verifier is full (R8) — after which every later target is minted,
// enqueued and left Queued. Chunking happens only when the pool delivers
// several targets at once; it is not the road.
type Survey struct {
	Repo     *git.Repo
	Ledger   *ledger.Ledger
	State    *statestore.Store
	Stage    run.Stager
	Local    run.Local
	Verifier func(context.Context) (verify.Verifier, error)
	Me       record.OwnerID
	Now      func() time.Time
	Progress progress.Sink
}

// Planned is one target as the pool delivered it: prepared, or declined
// at plan (Decline set, Prepared empty). Slug and Riders travel beside
// the Prepared for the reason ChangeRequest carries them — a Minting
// needs both and a Prepared holds neither.
type Planned struct {
	Target   string
	Prepared change.Prepared
	Slug     string
	Riders   []string
	Decline  error
}

// SurveyRequest: refused by arity are --replace, --diff, --in-place,
// --closes, --to, --riders' cohort forms, --to-pr, --timeout. Admission is
// --max-queued / --max-per-sweep (spelling open, Q10). Next is the
// pool's delivery, one target at a time, ok false when the selector is
// exhausted; cli runs planning behind it.
type SurveyRequest struct {
	Next      func(ctx context.Context) (Planned, bool)
	Delivery  Delivery // Document (outdated/classify), Branch (--no-verify) or Enqueue
	Platform  platform.Release
	Test      bool
	KeepEnv   bool
	Admission run.Admission
	InFlight  InFlight // Advance or Supersede
	Prov      change.Provenance
}

// Row is one target's outcome over a closed set.
type Row struct {
	Target   string
	Result   Result
	Withheld *run.NotAdmitted // admission refused it; no record exists
	Decline  error            // exit-10 family, quiet on a sweep
	Hard     error            // band 83
}

// Sweep is the sweep's own value and its own partition (F8): declined,
// refused and verdict are QUIET; 83 for hard errors; 60 if any attempt
// stayed queued; 0 if every one started or --no-verify. Never Pass's.
type Sweep struct {
	Rows  []Row
	Added int // this sweep's own count, the MaxPerPass unit
}

// Exit is the sweep's band, and it is deliberately NOT a maximum over
// the rows' own codes: a decline and a verdict are quiet on a sweep,
// where on a single target they are the answer.
func (s Sweep) Exit() int {
	queued, hard := false, false
	for _, r := range s.Rows {
		if r.Hard != nil {
			hard = true
		}
		if r.Result.Did == Queued {
			queued = true
		}
	}
	switch {
	case hard:
		return 83
	case queued:
		return 60
	}
	return 0
}

// Run walks the pool until it is exhausted, landing one target at a
// time. A hard error becomes a row and never a stop: the sweep's whole
// contract is that what it committed stands and a rerun resumes.
func (s Survey) Run(ctx context.Context, r SurveyRequest) (Sweep, error) {
	var sw Sweep
	if r.Next == nil {
		return sw, nil
	}
	prov, provErr := provider(ctx, s.Verifier)
	enqueue := r.Delivery == Enqueue && !isNoProvider(provErr)
	full := provErr != nil
	for {
		t, ok := r.Next(ctx)
		if !ok {
			return sw, nil
		}
		if t.Decline != nil {
			sw.Rows = append(sw.Rows, Row{Target: t.Target, Decline: t.Decline})
			continue
		}
		if r.Delivery == Document {
			sw.Rows = append(sw.Rows, Row{Target: t.Target, Result: Result{Did: Shown}})
			continue
		}
		row, err := s.one(ctx, r, t, prov, enqueue, &full, sw.Added)
		if err != nil {
			row = Row{Target: t.Target, Hard: err}
		}
		if row.Withheld == nil && row.Hard == nil && row.Result.Did != Stood {
			sw.Added++
		}
		if row.Result.Did == Minted && isNoProvider(provErr) {
			// the sweep's advisory, said once per minted row: the branch
			// exists and nothing on this host can verify it. It is set here
			// rather than in the per-target road because the fact is the
			// sweep's — one presence check, before the first target — and a
			// road that re-asked per target could answer differently
			// halfway down a selector.
			row.Result.Deferred = &Deferral{Reason: NoProvider, Detail: "unverified; install tart and `dockhand verify`"}
		}
		sw.Rows = append(sw.Rows, row)
		say(s.Progress, progress.Detail, t.Target) // the row lands as it lands
	}
}

// one is the per-target road, written out because its Amend's order is
// the claim: admit, release the old name (its branch's delete line
// queued), mint (the new branch's create line), enqueue — one batch.
func (s Survey) one(ctx context.Context, r SurveyRequest, t Planned, prov verify.Verifier, enqueue bool, full *bool, added int) (Row, error) {
	p := t.Prepared
	if len(p.Subjects) == 0 {
		return Row{}, ErrNothingPrepared
	}
	st, err := readBeforeMint(ctx, s.State)
	if err != nil {
		return Row{}, err
	}
	old, hasOld := change.Standing(st, p.Subjects[0].Port)
	if hasOld && r.InFlight == Advance {
		return Row{Target: t.Target, Result: Result{Did: Stood}}, nil // resume by rerun
	}
	// supersede: exactly Change.Run's order — the old change's live work
	// stopped and its kept environments released over the RECORD's id and
	// tip, then the checked-out observation, then the commit. It runs
	// before the Amend because the batch deletes the old branch (or, on a
	// same-name re-mint, re-points it at the NEW change), and a stage that
	// resolved it afterwards would find nothing, or the new change.
	if hasOld && r.InFlight == Supersede {
		if prov != nil {
			cancel := Cancel{Repo: s.Repo, Ledger: s.Ledger, State: s.State, Verifier: s.Verifier, Local: s.Local, Me: s.Me, Now: s.Now, Progress: s.Progress}
			if _, err := cancel.run(ctx, st, old.ID, old.Tip); err != nil {
				return Row{}, err
			}
		}
		if old.Branch != "" {
			wt, err := s.Repo.CheckedOutAt(ctx, old.Branch)
			if err != nil {
				return Row{}, err // could not read the worktree list (rule 7)
			}
			if wt != "" {
				return Row{Target: t.Target, Hard: change.ErrCheckedOut}, nil // a row, quiet on the sweep's own partition
			}
		}
	}
	sha, content, err := change.Commit(ctx, s.Repo, p, p.Base.Sha)
	if err != nil {
		return Row{}, err
	}
	m := change.Minting{
		ID: newChangeID(), Branch: branchFor(t.Slug), Tip: sha, Content: content,
		Subjects: p.Subjects, Crossing: change.Cross(p), Destination: destination(r.Delivery),
		Closes: p.Closes, Findings: p.Findings, Riders: t.Riders, Slug: t.Slug,
		Base: p.Base, Prov: r.Prov,
	}
	var att record.Attempt
	var withheld *run.NotAdmitted
	if err := s.State.Amend(ctx, func(tx *statestore.Txn) error {
		withheld, att = nil, record.Attempt{}
		if ok, why := run.Admit(run.Count(tx.State()), added, r.Admission); !ok {
			withheld = &run.NotAdmitted{Target: t.Target, Why: why}
			return nil // nothing written: no record, no branch, no attempt
		}
		if hasOld && r.InFlight == Supersede {
			if err := supersedeIn(tx, old, m.ID, s.Me, s.Now()); err != nil {
				return err
			}
		}
		if _, err := change.MintIn(tx, m, s.Now()); err != nil {
			return err
		}
		if !enqueue {
			return nil
		}
		seated, requires := buildOrder(ctx, s.Local, rosterOf(p.Subjects))
		spec := run.Spec{
			Content: content, Roster: seated, Requires: requires,
			FromSource: fromSourceOf(p.Subjects),
			Platform:   r.Platform, Test: r.Test, KeepEnv: r.KeepEnv,
		}
		var err error
		att, err = run.EnqueueIn(tx, run.Enqueue{
			Change: m.ID, Sha: sha, Content: content, Spec: spec, Platform: r.Platform,
			Ask: record.Ask{Test: r.Test, KeepEnv: r.KeepEnv}, EnqueuedBy: s.Me,
		}, s.Now())
		return err
	}); err != nil {
		return Row{}, err
	}
	if withheld != nil {
		return Row{Target: t.Target, Withheld: withheld}, nil
	}
	// resolve: the batch created the branch (and deleted the old one);
	// app holds no Ref it did not resolve.
	ref, err := change.Resolve(ctx, s.Repo, s.State, m.Branch)
	if err != nil {
		return Row{}, err
	}
	row := Row{Target: t.Target, Result: Result{Did: Minted, Ref: ref}}
	if !enqueue {
		return row, nil
	}
	row.Result.Did, row.Result.Attempt = Queued, att.ID
	if *full {
		return row, nil // flow 3: "38 attempts written, 2 submitted, 36 waiting"
	}
	started, err := run.Start(ctx, s.State, prov, s.Stage, att, s.Claimant(), s.Now())
	switch {
	case err == nil:
		row.Result.Did, row.Result.Lease = Started, leaseOf(started)
	case isNoVacancy(err):
		*full = true // the sentinel is the authority; nothing more starts in this process
	case isNoEnvironment(err):
		row.Result.Deferred = &Deferral{Reason: NoEnvironment, Detail: err.Error()}
	default:
		// A start that failed for this attempt's own reasons wrote its
		// backoff in run.Defer and is a ROW, not a stop: the branch and the
		// queued attempt stand, and the next pass will meet them.
		row.Result.Deferred = &Deferral{Reason: ProviderError, Detail: err.Error()}
	}
	return row, nil
}

// Claimant is who this sweep is, for the leases its starts acquire.
func (s Survey) Claimant() lease.Claimant { return lease.Claimant{Owner: s.Me} }
