package engine

import (
	"slices"

	"github.com/herbygillot/dockhand/internal/history"
	"github.com/herbygillot/dockhand/internal/model"
)

// history is how tidy, rebase, and restore change a branch's history, as
// complete transitions (history.Transitions).
func (e *Engine) history() *history.Transitions {
	return &history.Transitions{Repo: e.Repo, Store: e.Store, Repository: e.Repository, Now: e.now, Worktree: e.worktree, StopAt: e.stopAt}
}

// named reports whether a checkpoint name, tidy-3, is one of the kinds.
func named(kind string) bool {
	return slices.Contains([]string{string(model.CheckpointTidy), string(model.CheckpointRebase)}, kind)
}
