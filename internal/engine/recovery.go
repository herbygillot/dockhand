package engine

import (
	"context"
	"fmt"
	"strings"

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
func (e *Engine) ResumePrepared(ctx context.Context, where Where, tests model.TestPolicy, queue bool) ([]Resumed, error) {
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
		done := Resumed{Branch: branch}
		// Nothing is asked of Git by a name remembered before the branch's
		// lock: a rename cut short is finished under it first, and the
		// branch read as it stands (Codex's read of 2c, P2-1).
		if branch.RenameTo != "" {
			if err := e.lockedBranch(ctx, branch.ID, branch.Name, func(ctx context.Context, current model.Branch, _ *heldLock) error {
				branch = current
				return e.finishRename(ctx, &branch)
			}); err != nil {
				done.Branch, done.Problem = branch, err.Error()
				resumed = append(resumed, done)
				continue
			}
			done.Branch = branch
		}
		// serve's own preparation that stopped part-way is continued: its
		// unwritten edits settled, each target with no edit prepared at
		// the release kept for it, and its edits committed once nothing
		// is owed; what can't be finished is left and said, never
		// committed (#194, C3).
		if finished, problem := e.continuePreparation(ctx, &branch); problem != "" {
			done.Branch, done.Problem = branch, problem
			resumed = append(resumed, done)
			continue
		} else if finished {
			done.Branch = branch
		}
		head, _, err := e.Repo.Branch(ctx, branch.Name)
		if err != nil {
			done.Problem = fmt.Sprintf("reading %s's Git branch: %v", branch.ShortName(), err)
			resumed = append(resumed, done)
			continue
		}
		if model.ObjectID(head) == branch.Base || !queue {
			continue
		}
		// What ship would queue, recovery queues: neither a branch with
		// hand edits nor one held for a patch (finding 2).
		if prepared, err := e.preparation(ctx, branch, false); err != nil || prepared.Disposition != branchstate.ReadyToCheck {
			done.HandEdits = err == nil && prepared.Disposition == branchstate.HandEditsToCarry
			done.Held = err == nil && prepared.Disposition == branchstate.HeldBeforeCheck
			done.Problem = preparedWords(prepared, branch, err)
			resumed = append(resumed, done)
			continue
		}
		// Its commits are exactly dockhand's: the newest tidy applied and
		// not put back made the head, and every changed path is the chain
		// of its written edits from the base, as tidy tells them; a commit
		// on top, a rebase, or a person's change between two edits leaves
		// it, named (#194, C4 to C8).
		if why, err := e.committedAsPrepared(ctx, branch, head); err != nil || why != "" {
			done.Problem = why
			if err != nil {
				done.Problem = err.Error()
			}
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

// continuePreparation continues serve's own preparation that stopped
// part-way (#194, C3, H3): its unwritten edits settled as their own
// commands settle them, each target with no edit prepared at the release
// kept for it, and the edits committed once nothing is owed. finished
// says it committed; problem says what it couldn't finish, which is left
// and never committed.
func (e *Engine) continuePreparation(ctx context.Context, branch *model.Branch) (finished bool, problem string) {
	prepared, err := e.preparation(ctx, *branch, false)
	if err != nil {
		return false, err.Error()
	}
	if prepared.Disposition != branchstate.UnfinishedPreparation {
		return false, ""
	}
	if _, err := e.settled(ctx, branch, SettlerFor(SettlerOwner)); err != nil {
		return false, err.Error()
	}
	open, err := e.UnfinishedTargets(ctx, *branch)
	if err != nil {
		return false, err.Error()
	}
	for _, target := range open {
		request := UpdateRequest{Branch: *branch, Action: model.EditUpdate, Port: target.Port, Version: target.Named, Release: target.Release, CompareUpstream: true, Batch: true, Unattended: true}
		if target.Release != nil {
			request.Version = target.Release.Version
		}
		update, err := e.Update(ctx, request)
		if err != nil {
			return false, fmt.Sprintf("%s: %v", target.Port, err)
		}
		if update.Branch.ID != "" {
			*branch = update.Branch
		}
	}
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		*branch, err = r.Branch(branch.ID)
		return err
	}); err != nil {
		return false, err.Error()
	}
	if prepared, err = e.preparation(ctx, *branch, false); err != nil || prepared.Disposition == branchstate.UnfinishedPreparation && prepared.Pending || prepared.Disposition == branchstate.HandEditsToCarry {
		return false, preparedWords(prepared, *branch, err)
	}
	if prepared.Disposition != branchstate.UnfinishedPreparation {
		return false, ""
	}
	tidy, err := e.PlanTidy(ctx, TidyRequest{Branch: *branch})
	if err != nil {
		return false, err.Error()
	}
	if err := commitUnasked(shipOptions{Origin: model.OriginServe, Answered: model.AnsweredNobody}, tidy); err != nil {
		return false, err.Error()
	}
	if _, err := e.ApplyTidy(ctx, tidy); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// committedAsPrepared is why a branch's commits aren't exactly what
// dockhand committed of its edits, empty where they are (#194, C4 to C8):
// the newest history change applied is a tidy, not put back, that made
// the head; and every path the head changes from the base lies in a port
// directory whose files are the chain of the branch's written edits, as
// tidy itself tells them (fromEdits), modes included. A path outside any
// port's directory, a directory with no edit, a commit on top or a rebase
// leaves the branch, named.
func (e *Engine) committedAsPrepared(ctx context.Context, branch model.Branch, head string) (string, error) {
	var checkpoints []model.Checkpoint
	var edits []model.Edit
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if checkpoints, err = r.Checkpoints(branch.ID); err != nil {
			return err
		}
		edits, err = r.Edits(branch.ID)
		return err
	}); err != nil {
		return "", err
	}
	var newest *model.Checkpoint
	for i := range checkpoints {
		if checkpoints[i].State == model.CheckpointApplied {
			newest = &checkpoints[i]
		}
	}
	if newest == nil || newest.Kind != model.CheckpointTidy || newest.RestoredAt != nil || string(newest.After) != head {
		return fmt.Sprintf("%s's head isn't the commit dockhand's tidy made of its edits; it's left for you", branch.ShortName()), nil
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(branch.Base), head})
	if err != nil {
		return "", err
	}
	base, final := trees[string(branch.Base)], trees[head]
	changed, err := e.Repo.ChangedPaths(ctx, base, final)
	if err != nil {
		return "", err
	}
	byDirectory := map[string][]string{}
	for _, path := range changed {
		byDirectory[groupOf(path)] = append(byDirectory[groupOf(path)], path)
	}
	for directory, paths := range byDirectory {
		if directory == "" {
			return fmt.Sprintf("%s changes %s, outside any port's directory; it's left for you", branch.ShortName(), strings.Join(paths, ", ")), nil
		}
		chained, _, err := fromEdits(ctx, e.Repo, base, final, directory, paths, edits)
		if err != nil {
			return "", err
		}
		if !chained {
			return fmt.Sprintf("%s's %s isn't exactly what dockhand's edits wrote; it's left for you", branch.ShortName(), directory), nil
		}
	}
	return "", nil
}
