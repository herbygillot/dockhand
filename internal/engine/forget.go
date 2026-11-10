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
	// KeptWorktree is the branch's checkout that stays, one dockhand
	// didn't make: the person's own, as an adopted or --here branch
	// records, which archive leaves too.
	KeptWorktree string
	Head         string
	Unmerged     []git.CherryCommit
	// Fork is what the branch left on your fork, as clean offers it
	// (planCleanFork): removed before the record, whose checks name the
	// check branches, is forgotten; a step kept is said and left.
	Fork []CleanStep
	// Leftovers are what the branch's checks left in their providers, the
	// clones they kept above all, removed before the record, with which
	// no check of this checkout would claim them (BranchLeftovers).
	Leftovers []Leftover
}

// PlanForget is what forgetting branch would remove; a merged, closed, or
// archived branch is forgotten, so a person can end a merged branch's
// listing that clean keeps (field testing's xapian-core-dsq3), but not an
// open one, which dockhand archive sets aside first.
func (e *Engine) PlanForget(ctx context.Context, branch model.Branch) (ForgetPlan, error) {
	plan := ForgetPlan{Branch: branch}
	if branch.State == model.BranchOpen {
		return plan, fmt.Errorf("%s is open; clean --delete forgets a merged, closed, or archived branch, and dockhand archive sets an open one aside; %s", branch.ShortName(), ForgetWords(branch))
	}
	if branch.Worktree != "" && exists(branch.Worktree) {
		if !branch.Managed {
			plan.KeptWorktree = branch.Worktree
		} else {
			plan.Worktree = branch.Worktree
			if err := e.refuseDirty(ctx, branch, plan.Worktree); err != nil {
				return plan, err
			}
		}
	}
	if err := e.refuseCheckedOut(ctx, plan); err != nil {
		return plan, err
	}
	var err error
	if plan.Leftovers, err = e.BranchLeftovers(ctx, branch.ID); err != nil {
		return plan, err
	}
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
	if err := e.refuseCheckedOut(ctx, *plan); err != nil {
		return store.Pruned{}, err
	}
	// What its checks left goes first: one that stays, its check running
	// or its removal failing, keeps the record, which alone claims it.
	if len(plan.Leftovers) > 0 {
		if err := e.removeBranchLeftovers(ctx, plan); err != nil {
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

// removeBranchLeftovers removes what a branch's checks left, under each
// check's lease, refusing to go on, the record kept, where one stays.
func (e *Engine) removeBranchLeftovers(ctx context.Context, plan *ForgetPlan) error {
	session, err := e.Session(ctx, model.SessionForeground)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	removed, err := e.RemoveLeftovers(ctx, session, plan.Leftovers)
	plan.Leftovers = removed
	if err != nil {
		return refusal.New(refusal.Attention, "%s's record is kept, since what its checks left wasn't all removed: %v; run clean --delete again", plan.Branch.ShortName(), err)
	}
	for _, leftover := range removed {
		if !leftover.Done {
			return refusal.New(refusal.Attention, "%s's record is kept, since %s stays (%s); run clean --delete again once it's done", plan.Branch.ShortName(), leftover.What, leftover.Kept)
		}
	}
	return nil
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

// refuseCheckedOut refuses to forget a branch whose Git branch is checked
// out in a checkout the forgetting doesn't remove, as git branch -d
// refuses it: deleting the branch would leave that checkout on a branch
// that's gone, which update-ref, unlike git branch, doesn't check.
func (e *Engine) refuseCheckedOut(ctx context.Context, plan ForgetPlan) error {
	checkouts, err := e.Repo.AllCheckouts(ctx)
	if err != nil {
		return err
	}
	for _, checkout := range checkouts[plan.Branch.Name] {
		if plan.Worktree != "" && samePath(checkout, plan.Worktree) {
			continue
		}
		return refusal.New(refusal.Attention, "%s is checked out in %s, which clean --delete doesn't remove, so it isn't forgotten; git switch to another branch there, then run clean --delete again", plan.Branch.Name, checkout)
	}
	return nil
}
