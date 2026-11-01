package engine

import (
	"context"
	"slices"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Recovery is the check-execution service's, in one place: a run left
// running by a driver that's gone is a candidate serve takes first
// (Candidates), and a branch serve prepared and committed, whose check it
// never queued, has its check queued as serve starts (ResumePrepared),
// where the second was ship's and the first the runner's (Codex's review
// of 275125ff, finding 4).

// Candidates are the runs serve could drive, in the order it takes them:
// a run left running by a driver that is gone, then queued runs a person
// asked for, then serve's own, oldest first. Runs a live session holds,
// this one included, are left out.
func (c *checkService) Candidates(ctx context.Context, session *coord.Session) ([]model.Run, error) {
	var runs []model.Run
	var holders map[string]*model.Session
	// The runs and their holders in one read (SQL-O4), every 2 s in serve.
	if err := c.store.View(ctx, c.repository, func(r store.Reader) error {
		var err error
		if runs, err = r.Runs(store.RunFilter{States: []model.RunState{model.RunQueued, model.RunRunning}}); err != nil {
			return err
		}
		holders, err = session.Holders(r, runResources(runs)...)
		return err
	}); err != nil {
		return nil, err
	}
	rank := func(run model.Run) int {
		switch {
		case run.State == model.RunRunning:
			return 0
		case run.Origin == model.OriginPerson:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(runs, func(a, b model.Run) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return a.Number - b.Number
	})
	var candidates []model.Run
	for _, run := range runs {
		if holders[RunResource(run.ID)] == nil {
			candidates = append(candidates, run)
		}
	}
	return candidates, nil
}

// Resumed is a branch serve prepared and never queued a check of, as
// ResumePrepared found it: the run it queued, or why it was left for the
// person.
type Resumed struct {
	Branch model.Branch
	Run    *model.Run
	// Problem is why it wasn't queued; HandEdits that its worktree holds
	// edits not committed, which are the person's to carry on.
	Problem   string
	HandEdits bool
	// Held is a branch ship held before its check, a patch not applying.
	Held bool
}

// ResumePrepared is recovery's half for a branch, beside Candidates' for a
// run: it queues the check of each branch serve prepared and committed
// whose check it never queued: open, serve's, with commits of
// its own, no pull request, no run, and nothing uncommitted in its
// worktree. serve's preparation edits, commits, and starts a branch
// before it queues the check, so a serve stopped between them, or a
// planning error after the commit, left a branch the next serve skipped as
// already in hand, with no run for the queue's recovery or the submit
// pass to find (Codex's review of 2ad896d2, W6). One with uncommitted
// edits is left for the person, said.
func (c *checkService) ResumePrepared(ctx context.Context, where Where, tests model.TestPolicy) ([]Resumed, error) {
	var branches []model.Branch
	ran := map[model.BranchID]bool{}
	if err := c.store.View(ctx, c.repository, func(r store.Reader) error {
		var err error
		if branches, err = r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}}); err != nil {
			return err
		}
		runs, err := r.Runs(store.RunFilter{})
		for _, run := range runs {
			ran[run.Branch] = true
		}
		return err
	}); err != nil {
		return nil, err
	}
	var resumed []Resumed
	for _, branch := range branches {
		if branch.Origin != model.OriginServe || branch.PullRequest != nil || ran[branch.ID] || ctx.Err() != nil {
			continue
		}
		head, _, err := c.work.checkout().Branch(ctx, branch.Name)
		if err != nil || model.ObjectID(head) == branch.Base {
			continue
		}
		done := Resumed{Branch: branch}
		// What ship would queue, recovery queues: neither a branch with
		// hand edits nor one ship held for a patch (finding 2).
		if prepared, err := c.work.preparation(ctx, branch); err != nil || prepared.Disposition != branchstate.ReadyToCheck {
			done.HandEdits = err == nil && prepared.Disposition == branchstate.HandEditsToCarry
			done.Held = err == nil && prepared.Disposition == branchstate.HeldBeforeCheck
			done.Problem = preparedWords(prepared, branch, err)
			resumed = append(resumed, done)
			continue
		}
		proposal, err := c.work.ProposeCheck(ctx, CheckRequest{Capture: CaptureRequest{Branch: branch, Mode: CaptureHead}, Where: where, Tests: tests})
		if err == nil {
			var queued Queued
			if queued, err = c.QueueCheck(ctx, QueueRequest{Branch: branch, Revision: proposal.Capture.Revision, Plan: proposal.Plan, Origin: model.OriginServe}); err == nil {
				done.Run = &queued.Run
			}
		}
		if err != nil {
			done.Problem = err.Error()
		}
		resumed = append(resumed, done)
	}
	return resumed, nil
}
