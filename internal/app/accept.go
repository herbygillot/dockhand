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

// Accept is the operation behind `bump-revision --for <branch>` (Q22:
// Accept IS this road), and the grid's Accept column corrected: it
// EXTENDS the branch that already carries the change, so its ticks are
// extend + answer, never begin or mint. dismiss is NOT here — it is
// change.AnswerIn with Dismissed, opened by the Dismiss entry point (one
// lifecycle, R22). The proposal it answers was WRITTEN by run.Finish's
// propose step (change.ProposeIn) when the headline's verification
// settled; resolve reads the stored finding and never re-derives it.
type Accept struct {
	Repo      *git.Repo
	Ledger    *ledger.Ledger
	State     *statestore.Store
	Stage     run.Stager
	Local     run.Local
	Verifier  func(context.Context) (verify.Verifier, error)
	Me        record.OwnerID
	Residency func(context.Context) Residency
	Now       func() time.Time
	Progress  progress.Sink
	// Prepare is the cohort's plan-and-prepare seam: the WHOLE cohort's
	// content in one Prepared, planned from the TIP's blobs rather than
	// from the working tree, so a cohort commit re-declares exactly what
	// the branch already carries.
	//
	// IT IS A FUNCTION AND NOT A CALL INTO planning, and the reason is
	// this operation's own done-criterion. Preparing a member needs the
	// base commit's Portfile bytes read out of git at the RESOLVED TIP,
	// an evaluator to hold the prediction against, and the intent's
	// parameters — three things Accept would otherwise have to hold and
	// two of them (a blob read and an evaluation) are exactly what "no
	// operation opens a file" forbids. cli implements it over the same
	// planning.Planner this operation carries for provenance.
	//
	// IT TAKES THE WHOLE CANDIDATE LIST AND RETURNS ONE VALUE, which the
	// sketch's app-private prepareCohort did not, and the difference is a
	// MEASURED limit of change.Prepared rather than a preference:
	// Prepared carries ONE Portdir and change's materialize joins every
	// File.Path under it (internal/change/change.go:118-131), so a
	// Prepared per member cannot be concatenated — the second member's
	// files would land under the first member's directory — and a cohort
	// spanning several portdirs is not expressible in one Prepared as the
	// type stands. Putting the whole cohort behind one seam keeps that
	// gap at one visible boundary instead of hiding a wrong join inside
	// this operation, and keeps Accept's contribution what it is: the
	// SEQUENCE, not the preparation.
	//
	// criterion is the MEASUREMENT the proposal rests on, verbatim, and it
	// is a parameter because it is what the revbump commits must state:
	// "why users must rebuild". It lives on the finding rather than on any
	// candidate — a candidate's own Reason says why that port is in the
	// cohort ("depends_lib"), which is a different sentence for a
	// different reader.
	Prepare func(ctx context.Context, tip string, cands []record.Candidate, criterion string) (change.Prepared, error)
}

// AcceptRequest is what one `bump-revision --for` asked. Platform is
// singular for the reason ChangeRequest's is: the invocation-level
// plural lives on VerifyRequest and nowhere else.
type AcceptRequest struct {
	Branch        string
	Exclude       []string
	ForceWithheld []string
	NoVerify      bool
	Test          bool
	KeepEnv       bool
	Platform      platform.Release
	Wait          *time.Duration
	Residency     Residency
	Prov          change.Provenance
}

// Run: resolve (the one Proposed cohort finding off the record; three
// NoProposal refusals, exit 10; a person's hold refused, exit 23;
// ErrTipDisagrees is 45 — `verify` first); change.Cohort amends the
// candidates (five declines); plan each member from the TIP's blobs;
// prepare; commit with parent = the resolved tip; checked out?
// (git.Repo.CheckedOutAt — the update line ExtendIn queues MOVES a
// branch a worktree has checked out under that worktree's index,
// measured, where `branch -f` refuses; change.ErrCheckedOut, 46, switch
// away first; a failed read is its own error); ONE Amend: ExtendIn with
// ExpectedTip = that tip (ErrTipMoved is the record-level CAS, the
// update line the ref-level one) + AnswerIn (+ EnqueueIn unless
// --no-verify); resolve — the Ref app returns is the witness that the
// batch landed; supersede over the OLD tip's attempts; start; watch by
// residency. The lease slot keys on ChangeID, which is what makes this
// road cheap: the sha moves, the identity does not.
func (a Accept) Run(ctx context.Context, r AcceptRequest) (Result, error) {
	ref, err := change.Resolve(ctx, a.Repo, a.State, r.Branch)
	if err != nil {
		return Result{}, err
	}
	st, err := a.State.Read(ctx)
	if err != nil {
		return Result{}, err
	}
	c := st.Changes[string(ref.ID())]
	if err := change.Held(c, change.ActVerify, record.Human); err != nil {
		return Result{}, err
	}
	candidates, err := change.Cohort(c, r.Exclude, r.ForceWithheld)
	if err != nil {
		return Result{}, err
	}
	// plan + prepare each member from the tip's blobs (a member that
	// declines is named and the cohort proceeds), then one Prepared for
	// the cohort commit.
	prepared, err := a.prepareCohort(ctx, ref, candidates, change.Criterion(c))
	if err != nil {
		return Result{}, err
	}
	sha, content, err := change.Commit(ctx, a.Repo, prepared, ref.Tip())
	if err != nil {
		return Result{}, err
	}
	wt, err := a.Repo.CheckedOutAt(ctx, ref.Branch())
	if err != nil {
		return Result{}, err
	}
	if wt != "" {
		return Result{}, change.ErrCheckedOut // 46: the cohort commit is garbage; nothing was written
	}
	prov, provErr := provider(ctx, a.Verifier)
	enqueue := !r.NoVerify && !isNoProvider(provErr)
	var att record.Attempt
	// spec is captured from INSIDE the closure, because the roster a
	// cohort enqueues is run.Roster over the change AS THE ANSWER LEAVES
	// IT: ExtendIn adds the members and AnswerIn marks the finding
	// Accepted, and a roster computed before either would seat a withheld
	// member and hash to a spec id no drain could re-derive.
	var spec run.Spec
	if err := a.State.Amend(ctx, func(tx *statestore.Txn) error {
		att, spec = record.Attempt{}, run.Spec{}
		if _, err := change.ExtendIn(tx, change.Extension{ID: ref.ID(), ExpectedTip: ref.Tip(), Tip: sha, Content: content, Subjects: prepared.Subjects, Prov: r.Prov}, a.Now()); err != nil {
			return err
		}
		if err := change.AnswerIn(tx, ref.ID(), record.KindABIDependents, record.Accepted, candidates, a.Now()); err != nil {
			return err
		}
		if !enqueue {
			return nil
		}
		cur := tx.State().Changes[string(ref.ID())]
		members, withheld := run.Roster(cur, record.Attempt{})
		seated, requires := buildOrder(ctx, a.Local, members)
		spec = run.Spec{
			Content: content, Roster: seated, Requires: requires, Withheld: withheld,
			// Over the change's OWN subjects and not over the seated
			// members: a cohort accepted onto a re-derivation still has to
			// build that headline from source, and run.Plan intersects the
			// list with the ports actually being built, so naming a
			// withheld one costs nothing.
			FromSource: fromSourceOf(cur.Subjects),
			Platform:   r.Platform, Test: r.Test, KeepEnv: r.KeepEnv,
		}
		var err error
		att, err = run.EnqueueIn(tx, run.Enqueue{Change: ref.ID(), Sha: sha, Content: content, Spec: spec, Platform: r.Platform, Ask: record.Ask{Test: r.Test, KeepEnv: r.KeepEnv}, EnqueuedBy: a.Me}, a.Now())
		return err
	}); err != nil {
		return Result{}, err
	}
	// the note, over the state this road leaves: the cohort commit is the
	// change's tip now, and the note on it is what a reviewer reads.
	defer func() { exportNote(ctx, a.State, a.Ledger, sha, a.Progress) }()
	// resolve: the batch moved the branch; app holds no Ref it did not
	// resolve. A foreign move in the second between is reported here.
	newRef, err := change.Resolve(ctx, a.Repo, a.State, r.Branch)
	if err != nil {
		return Result{}, err
	}
	res := Result{Did: Minted, Ref: newRef}
	if provErr == nil {
		st, err := a.State.Read(ctx)
		if err != nil {
			return res, err
		}
		if err := supersede(ctx, a.State, a.Ledger, prov, a.Local, st, newRef.ID(), newRef.Tip(), a.Claimant(), a.Me, a.Now); err != nil {
			return res, err
		}
	}
	if !enqueue {
		if isNoProvider(provErr) {
			res.Deferred = &Deferral{Reason: NoProvider, Detail: "unverified; install tart and `dockhand verify`"}
		}
		return res, nil
	}
	res.Did, res.Attempt = Queued, att.ID
	if provErr != nil {
		res.Deferred = &Deferral{Reason: ProviderError, Detail: provErr.Error()}
		return res, nil
	}
	started, err := run.Start(ctx, a.State, prov, a.Stage, att, a.Claimant(), a.Now())
	switch {
	case err == nil:
		res.Did, res.Lease = Started, leaseOf(started)
	case isNoVacancy(err):
		return res, nil
	case isNoEnvironment(err):
		res.Deferred = &Deferral{Reason: NoEnvironment, Detail: err.Error()}
		return res, nil
	default:
		return res, err
	}
	if r.Wait == nil {
		return res, nil
	}
	final, err := watch(ctx, a.State, a.Ledger, prov, a.Local, started, spec, *r.Wait, r.Residency, a.Residency, a.Claimant(), a.Now, a.Progress)
	if err == nil && final.Phase == record.Finished {
		res.Did, res.Verdict = Stood, verdictOf(final)
	}
	return res, err
}

// prepareCohort asks the seam for the cohort's content and refuses an
// answer with nothing in it.
//
// A cohort that prepared no subject at all is change.ErrEmptyCohort —
// the same decline change.Cohort raises when every candidate was
// excluded — because the two are one fact to a person: nothing was
// seated, and the branch is unchanged. It is raised HERE rather than
// left to change.Commit, which would happily write a commit identical to
// its parent and leave an extension recording a cohort that bumped
// nothing.
func (a Accept) prepareCohort(ctx context.Context, ref change.Ref, cands []record.Candidate, criterion string) (change.Prepared, error) {
	if a.Prepare == nil {
		return change.Prepared{}, ErrNothingPrepared
	}
	p, err := a.Prepare(ctx, ref.Tip(), cands, criterion)
	if err != nil {
		return change.Prepared{}, err
	}
	if len(p.Subjects) == 0 || len(p.Files) == 0 {
		return change.Prepared{}, change.ErrEmptyCohort
	}
	return p, nil
}

// Claimant is who this accept is, for the lease its start acquires.
func (a Accept) Claimant() lease.Claimant { return lease.Claimant{Owner: a.Me} }
