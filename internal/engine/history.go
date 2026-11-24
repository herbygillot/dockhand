package engine

import (
	"context"
	"slices"

	"github.com/herbygillot/dockhand/internal/history"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
)

// history is how tidy, rebase, and restore change a branch's history, as
// complete transitions (history.Transitions).
func (e *Engine) history() *history.Transitions {
	return &history.Transitions{Repo: e.Repo, Store: e.Store, Repository: e.Repository, Now: e.now, Worktree: e.worktree, StopAt: e.stopAt, Before: e.renameAtLock}
}

// named reports whether a checkpoint name, tidy-3, is one of the kinds.
func named(kind string) bool {
	return slices.Contains([]string{string(model.CheckpointTidy), string(model.CheckpointRebase)}, kind)
}

// renameAtLock finishes, at a history change's lock, a rename of the
// branch a process was cut short in (#194, L5). The change was planned
// under the old name, so where the name moves it stops, to be run again.
func (e *Engine) renameAtLock(ctx context.Context, branch model.Branch) error {
	var current model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		current, err = r.Branch(branch.ID)
		return err
	}); err != nil {
		return err
	}
	// The lock is of the name the change was planned under; one another
	// command moved the branch from since is the wrong lock (L7).
	if current.RenameTo == "" {
		if current.Name != branch.Name {
			return refusal.New(refusal.Attention, "%s was renamed to %s since this was planned; run the command again", branch.ShortName(), current.ShortName())
		}
		return nil
	}
	if err := e.finishRename(ctx, &current); err != nil {
		return err
	}
	return refusal.New(refusal.Attention, "%s was being renamed, and is %s now; run the command again", branch.ShortName(), current.ShortName())
}
