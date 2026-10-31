package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
)

// ForgetPlan is what forgetting an ended branch removes (clean
// --delete): its worktree, where one is left; its Git branch at Head,
// where it has one, with the commits on it the master last fetched lacks,
// Unmerged, which go with it; and its record, but for what other
// branches' results name of its checks.
type ForgetPlan struct {
	Branch   model.Branch
	Worktree string
	Head     string
	Unmerged []git.CherryCommit
	// Fork is what the branch left on your fork, as clean offers it
	// (planCleanFork): removed before the record, whose checks name the
	// check branches, is forgotten; a step kept is said and left.
	Fork []CleanStep
}

// PlanForget is what forgetting branch would remove; a merged, closed, or
// archived branch is forgotten, so a person can end a merged branch's
// listing that clean keeps (field testing's xapian-core-dsq3), but not an
// open one, which dockhand archive sets aside first.
func (e *Engine) PlanForget(ctx context.Context, branch model.Branch) (ForgetPlan, error) {
	plan := ForgetPlan{Branch: branch}
	if branch.State == model.BranchOpen {
		return plan, fmt.Errorf("%s is open; clean --delete forgets a merged, closed, or archived branch, and dockhand archive sets an open one aside", branch.ShortName())
	}
	if branch.Worktree != "" && exists(branch.Worktree) {
		plan.Worktree = branch.Worktree
		if err := e.refuseDirty(ctx, branch, plan.Worktree); err != nil {
			return plan, err
		}
	}
	var err error
	if plan.Fork, err = e.planCleanFork(ctx, branch, pushedHead(branch), "it has moved since dockhand pushed it"); err != nil {
		return plan, err
	}
	head, _, err := e.Repo.Branch(ctx, branch.Name)
	switch {
	case errors.Is(err, git.ErrBranchMissing):
		return plan, nil
	case err != nil:
		return plan, err
	}
	plan.Head = head
	// Commits whose change master has under another commit, as a rebase
	// merges them, aren't lost with the branch.
	if master, ok := e.lastMaster(ctx); ok {
		commits, err := e.Repo.Cherry(ctx, string(master), head)
		if err != nil {
			return plan, err
		}
		for _, commit := range commits {
			if !commit.Equivalent {
				plan.Unmerged = append(plan.Unmerged, commit)
			}
		}
	}
	return plan, nil
}

// Forget removes what plan says, worktree, Git branch, then record, the
// Git branch only while it's still at the commit planned, so one someone
// moved since stays, and the record isn't forgotten.
func (e *Engine) Forget(ctx context.Context, plan *ForgetPlan) (store.Pruned, error) {
	// The worktree's work may have come since the plan: nothing goes, the
	// fork's branch neither, and git's own removal refuses what comes
	// after this look.
	if plan.Worktree != "" {
		if err := e.refuseDirty(ctx, plan.Branch, plan.Worktree); err != nil {
			return store.Pruned{}, err
		}
	}
	if len(plan.Fork) > 0 {
		cleaned, err := e.ApplyClean(ctx, []CleanBranch{{Branch: plan.Branch, Steps: plan.Fork}})
		if err != nil {
			return store.Pruned{}, err
		}
		plan.Fork = cleaned[0].Steps
	}
	if plan.Worktree != "" {
		if err := e.Repo.RemoveUnchangedWorktree(ctx, plan.Worktree); err != nil {
			return store.Pruned{}, fmt.Errorf("removing %s's worktree: %w", plan.Branch.ShortName(), err)
		}
	}
	if plan.Head != "" {
		if err := e.Repo.DeleteBranch(ctx, plan.Branch.Name, plan.Head); err != nil {
			return store.Pruned{}, fmt.Errorf("deleting %s: %w", plan.Branch.Name, err)
		}
	}
	var pruned store.Pruned
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		var err error
		pruned, err = tx.ForgetBranch(plan.Branch.ID, e.now())
		return err
	})
	return pruned, err
}

// refuseDirty refuses to forget a branch whose worktree holds work of its
// own, uncommitted edits or untracked files, which its removal would lose,
// as clean and archive keep such a worktree; exit 3, a person's look.
func (e *Engine) refuseDirty(ctx context.Context, branch model.Branch, worktree string) error {
	dirty, err := e.dirty(ctx, worktree)
	if err != nil || dirty == "" {
		return err
	}
	return refusal.New(refusal.Attention, "%s's worktree %s is kept: %s, which forgetting it would lose; commit them or remove them, then run clean --delete again", branch.ShortName(), worktree, dirty)
}
