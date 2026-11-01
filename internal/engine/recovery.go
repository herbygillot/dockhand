package engine

import (
	"context"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Recovery of a branch is the engine's, beside the check-execution
// service's of a run (checks.Service.Candidates): a branch serve prepared
// and committed, whose check it never queued, has its check planned and
// queued as serve starts (ResumePrepared), as ship would (Codex's review
// of 275125ff, finding 4).

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
func (e *Engine) ResumePrepared(ctx context.Context, where Where, tests model.TestPolicy) ([]Resumed, error) {
	var branches []model.Branch
	ran := map[model.BranchID]bool{}
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
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
		head, _, err := e.Repo.Branch(ctx, branch.Name)
		if err != nil || model.ObjectID(head) == branch.Base {
			continue
		}
		done := Resumed{Branch: branch}
		// What ship would queue, recovery queues: neither a branch with
		// hand edits nor one ship held for a patch (finding 2).
		if prepared, err := e.preparation(ctx, branch); err != nil || prepared.Disposition != branchstate.ReadyToCheck {
			done.HandEdits = err == nil && prepared.Disposition == branchstate.HandEditsToCarry
			done.Held = err == nil && prepared.Disposition == branchstate.HeldBeforeCheck
			done.Problem = preparedWords(prepared, branch, err)
			resumed = append(resumed, done)
			continue
		}
		proposal, err := e.ProposeCheck(ctx, CheckRequest{Capture: CaptureRequest{Branch: branch, Mode: CaptureHead}, Where: where, Tests: tests})
		if err == nil {
			var queued checks.Queued
			if queued, err = e.QueueCheck(ctx, checks.QueueRequest{Branch: branch, Revision: proposal.Capture.Revision, Plan: proposal.Plan, Origin: model.OriginServe}); err == nil {
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
