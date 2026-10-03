package engine

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// followRenames carries the record of each open branch renamed with
// `git branch -m` over to its new name, as adopt does (renamedFrom,
// carryRename), without being asked, and says which by branch, with the
// name it had: its Git branch is gone, and its worktree has checked out
// a branch no record tracks, which renamedFrom finds it the one renamed
// from (the person's ruling on the M1's quick stage, D-S5, 2026-10-03:
// follow it). A branch two records could be stays untracked, as adopt
// leaves it. With a name, only a rename to that name is followed, as a
// command naming a branch finds it.
func (e *Engine) followRenames(ctx context.Context, name string) (map[model.BranchID]string, error) {
	var branches []model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		branches, err = r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}})
		return err
	}); err != nil {
		return nil, err
	}
	tracked := map[string]bool{}
	for _, branch := range branches {
		tracked[branch.Name] = true
	}
	followed := map[model.BranchID]string{}
	for _, branch := range branches {
		if branch.Worktree == "" || !exists(branch.Worktree) {
			continue
		}
		if _, _, err := e.Repo.Branch(ctx, branch.Name); !errors.Is(err, git.ErrBranchMissing) {
			continue
		}
		worktree, err := git.Open(ctx, branch.Worktree, e.options.Git)
		if err != nil {
			continue
		}
		current, err := worktree.CurrentBranch(ctx)
		if err != nil || current == "" || tracked[current] || name != "" && current != name && current != model.BranchPrefix+name {
			continue
		}
		head, _, err := e.Repo.Branch(ctx, current)
		if err != nil {
			continue
		}
		renamed, ok, err := e.renamedFrom(ctx, current, head)
		if err != nil || !ok || renamed.ID != branch.ID {
			continue
		}
		if _, err := e.carryRename(ctx, renamed, current); err != nil {
			return followed, err
		}
		tracked[current] = true
		followed[branch.ID] = branch.ShortName()
	}
	return followed, nil
}
