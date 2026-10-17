package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/history"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Restore puts a branch's history back as a checkpoint kept it, when the
// branch and its index are still where the tidy or rebase that made it
// left them. A tidy's puts the index back as it kept that, and leaves the
// working files alone, so edits tidy had committed read as uncommitted
// again. A rebase's puts back the files too, as they were before it, and
// the master the branch started from, in the transaction that marks it
// restored.
func (e *Engine) Restore(ctx context.Context, name string) (model.Checkpoint, model.Branch, error) {
	kind, digits, _ := strings.Cut(name, "-")
	number, err := strconv.Atoi(digits)
	if err != nil || !named(kind) {
		return model.Checkpoint{}, model.Branch{}, fmt.Errorf("%q is not a checkpoint name, such as tidy-3", name)
	}
	checkpoint, branch, err := e.checkpointNamed(ctx, name, kind, number)
	if err != nil {
		return checkpoint, branch, err
	}
	err = e.history().With(ctx, branch, func(ctx context.Context) error {
		// What a stopped dockhand left is settled now; read it again.
		if checkpoint, branch, err = e.checkpointNamed(ctx, name, kind, number); err != nil {
			return err
		}
		checkpoint, branch, err = e.restore(ctx, checkpoint, branch)
		return err
	})
	return checkpoint, branch, err
}

// LatestCheckpoint is the name of a branch's latest tidy or rebase that
// undo can take back: applied, and not restored since. Where it has none,
// the error says so.
func (e *Engine) LatestCheckpoint(ctx context.Context, branch model.Branch) (string, error) {
	var name string
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		checkpoints, err := r.Checkpoints(branch.ID)
		if err != nil {
			return err
		}
		for i := len(checkpoints) - 1; i >= 0; i-- {
			if c := checkpoints[i]; c.State == model.CheckpointApplied && c.RestoredAt == nil {
				name = c.Name()
				return nil
			}
		}
		return fmt.Errorf("%s has no tidy or rebase to undo", branch.ShortName())
	})
	return name, err
}

// checkpointNamed reads a checkpoint by its name's kind and number, and
// its branch.
func (e *Engine) checkpointNamed(ctx context.Context, name, kind string, number int) (model.Checkpoint, model.Branch, error) {
	var checkpoint model.Checkpoint
	var branch model.Branch
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		checkpoint, err = r.Checkpoint(number)
		if errors.Is(err, store.ErrNotFound) || err == nil && string(checkpoint.Kind) != kind {
			return fmt.Errorf("there is no checkpoint %s", name)
		}
		if err != nil {
			return err
		}
		branch, err = r.Branch(checkpoint.Branch)
		return err
	})
	return checkpoint, branch, err
}

// restore puts a checkpoint's history back, holding the branch's history
// lock (withHistory), and returns the checkpoint and branch as they are
// after.
func (e *Engine) restore(ctx context.Context, checkpoint model.Checkpoint, branch model.Branch) (model.Checkpoint, model.Branch, error) {
	name := checkpoint.Name()
	switch {
	case checkpoint.State == model.CheckpointAbandoned:
		return checkpoint, branch, fmt.Errorf("%s was not applied because dockhand stopped before making changes; there is nothing to restore", name)
	case checkpoint.State != model.CheckpointApplied:
		return checkpoint, branch, fmt.Errorf("%s has an unresolved state: %s is not checked out, so dockhand cannot determine whether the operation was applied", name, branch.Name)
	case checkpoint.RestoredAt != nil:
		return checkpoint, branch, fmt.Errorf("%s was already restored", name)
	}
	if err := checkpoint.Validate(); err != nil {
		return checkpoint, branch, err
	}
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return checkpoint, branch, err
	}
	head, _, err := worktree.Branch(ctx, branch.Name)
	if err != nil {
		return checkpoint, branch, err
	}
	if model.ObjectID(head) != checkpoint.After {
		return checkpoint, branch, fmt.Errorf("%s has moved on since %s (it is at %s, not %s); restoring would discard that work, so nothing was changed",
			branch.Name, name, model.ObjectID(head).Short(), checkpoint.After.Short())
	}
	if checkpoint.Index != "" {
		// The rewrite left the index at its new head's tree; anything
		// else is staged work that restoring would replace.
		index, err := worktree.IndexTree(ctx)
		if err != nil {
			return checkpoint, branch, err
		}
		trees, err := worktree.CommitTrees(ctx, []string{string(checkpoint.After)})
		if err != nil {
			return checkpoint, branch, err
		}
		if index != trees[string(checkpoint.After)] {
			return checkpoint, branch, fmt.Errorf("%s has changes staged after %s. Restoring would overwrite them, so nothing was changed. Commit or stash those changes before restoring", branch.Name, name)
		}
	}
	if checkpoint.Kind == model.CheckpointRebase {
		// A rebase ran with nothing uncommitted and left the files as the
		// rebased commit has them. They go back with the history, or
		// master's newer files would read as the branch's own edits.
		if err := worktree.MoveCheckout(ctx, string(checkpoint.After), string(checkpoint.Before)); err != nil {
			return checkpoint, branch, fmt.Errorf("restoring %s puts back master's older files too, and a change to one of them stops it, so nothing was changed. Commit it (dockhand tidy), or set it aside, first: %w", name, err)
		}
	} else {
		if err := worktree.UpdateRefs(ctx, []git.RefChange{{Name: "refs/heads/" + branch.Name,
			Expected: git.RefValue{Exists: true, Object: string(checkpoint.After)}, Desired: git.RefValue{Exists: true, Object: string(checkpoint.Before)}}}); err != nil {
			return checkpoint, branch, err
		}
		if checkpoint.Index != "" {
			if err := worktree.SetIndex(ctx, string(checkpoint.Index)); err != nil {
				return checkpoint, branch, err
			}
		} else if err := worktree.ResetIndex(ctx); err != nil {
			return checkpoint, branch, err
		}
	}
	if err := e.history().Step("moved"); err != nil {
		return checkpoint, branch, err
	}
	restored := e.now()
	checkpoint.RestoredAt = &restored
	after, err := e.history().RecordRestore(ctx, checkpoint, fmt.Sprintf("restored %s's history from %s", branch.Name, name))
	if err != nil {
		return checkpoint, branch, history.Unfinished("the history is restored", err)
	}
	return checkpoint, after, nil
}
