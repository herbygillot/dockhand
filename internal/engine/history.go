package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// A change to a branch's history, a tidy, rebase, or restore, is one
// complete transition (Design v3 §8). It holds the branch's lock
// throughout, so no other dockhand changes that history meanwhile, and
// never calls Git inside a transaction:
//
//   - a tidy or rebase computes its new commits, which nothing refers to
//     yet; records its checkpoint as prepared; makes its Git change, guarded
//     by compare-and-swap; and settles the checkpoint as applied, or as
//     abandoned when the change couldn't be made;
//   - a restore makes its Git change, then records it.
//
// A commit the store reports as uncertain is read back before anything
// more is done, and a Git change once made is never undone. A process
// that stops part-way leaves a state the next of these commands on the
// branch recognizes, from what Git shows, and finishes (settleHistory).

// withHistory runs fn holding the branch's history lock, after finishing
// whatever a stopped process left.
func (e *Engine) withHistory(ctx context.Context, branch model.Branch, fn func(context.Context) error) error {
	return e.Repo.WithBranchLock(ctx, branch.Name, func(ctx context.Context) error {
		if err := e.settleHistory(ctx, branch); err != nil {
			return fmt.Errorf("finishing what a stopped dockhand left of %s: %w", branch.ShortName(), err)
		}
		return fn(ctx)
	})
}

// historyStep is a point in a history change where a test stops it, as
// if the process ended there; nil outside tests.
func (e *Engine) historyStep(step string) error {
	if e.stopAt == nil {
		return nil
	}
	return e.stopAt(step)
}

// prepareCheckpoint records a checkpoint as prepared, before the change it
// keeps is made, and gives it its number.
func (e *Engine) prepareCheckpoint(ctx context.Context, checkpoint *model.Checkpoint) error {
	checkpoint.State = model.CheckpointPrepared
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		number, err := tx.NextCheckpointNumber()
		if err != nil {
			return err
		}
		checkpoint.Number = number
		return tx.AddCheckpoint(*checkpoint)
	})
	if errors.Is(err, store.ErrUncertain) {
		if recorded, found := e.readCheckpoint(ctx, checkpoint.Number); found && recorded.Branch == checkpoint.Branch && recorded.Before == checkpoint.Before && recorded.After == checkpoint.After {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("recording the checkpoint failed, so nothing was changed: %w", err)
	}
	return nil
}

// settleCheckpoint records a prepared checkpoint as applied or abandoned,
// with the event that says so. An applied rebase moves the branch's base
// in the same transaction.
func (e *Engine) settleCheckpoint(ctx context.Context, checkpoint model.Checkpoint, state model.CheckpointState, message string) error {
	checkpoint.State = state
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.SettleCheckpoint(checkpoint); err != nil {
			return err
		}
		if state == model.CheckpointApplied && checkpoint.Kind == model.CheckpointRebase {
			if err := e.setBase(tx, checkpoint.Branch, checkpoint.BaseAfter, checkpoint.Name()); err != nil {
				return err
			}
		}
		if message == "" {
			return nil
		}
		_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: checkpoint.Branch, Kind: "branch." + string(checkpoint.Kind), Level: model.LevelInfo, Message: message})
		return err
	})
	if errors.Is(err, store.ErrUncertain) {
		if recorded, found := e.readCheckpoint(ctx, checkpoint.Number); found && recorded.State == state {
			return nil
		}
	}
	return err
}

// recordRestore records a checkpoint's restore, and puts back the base the
// restored history starts from, in one transaction.
func (e *Engine) recordRestore(ctx context.Context, checkpoint model.Checkpoint, message string) (model.Branch, error) {
	var branch model.Branch
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.MarkRestored(checkpoint); err != nil {
			return err
		}
		current, err := tx.Branch(checkpoint.Branch)
		if err != nil {
			return err
		}
		// The history put back starts from the master it started from
		// then, whatever the branch's base is now.
		if checkpoint.BaseBefore != "" && current.Base != checkpoint.BaseBefore {
			message += fmt.Sprintf(", back onto master %s", short(checkpoint.BaseBefore))
			current.Base = checkpoint.BaseBefore
			if err := tx.UpdateBranch(current); err != nil {
				return err
			}
		}
		branch = current
		_, err = tx.AppendEvent(model.Event{At: *checkpoint.RestoredAt, Branch: checkpoint.Branch, Kind: "branch.restore", Level: model.LevelInfo, Message: message})
		return err
	})
	if errors.Is(err, store.ErrUncertain) {
		if recorded, found := e.readCheckpoint(ctx, checkpoint.Number); found && recorded.RestoredAt != nil {
			branch, err = e.Branch(ctx, checkpoint.Branch)
		}
	}
	return branch, err
}

// readCheckpoint reads a checkpoint back, as it is recorded now.
func (e *Engine) readCheckpoint(ctx context.Context, number int) (model.Checkpoint, bool) {
	var checkpoint model.Checkpoint
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		checkpoint, err = r.Checkpoint(number)
		return err
	})
	return checkpoint, err == nil
}

// setBase records a branch's new base, and the rebase that moved it.
func (e *Engine) setBase(tx store.Tx, id model.BranchID, base model.ObjectID, checkpoint string) error {
	current, err := tx.Branch(id)
	if err != nil {
		return err
	}
	previous := current.Base
	current.Base = base
	if err := tx.UpdateBranch(current); err != nil {
		return err
	}
	message := fmt.Sprintf("rebased %s from master %s onto %s", current.Name, short(previous), short(base))
	if checkpoint != "" {
		message += " (checkpoint " + checkpoint + ")"
	}
	_, err = tx.AppendEvent(model.Event{At: e.now(), Branch: id, Kind: "branch.rebase", Level: model.LevelInfo, Message: message})
	return err
}

// settleHistory finishes what a process that stopped part-way through a
// change to the branch's history left, from what Git shows. The caller
// holds the branch's lock.
//
//   - A prepared checkpoint whose change was made, the branch at its After
//     or past it, is applied, as the stopped process would have recorded
//     it; a tidy's index is reset if it still holds what the tidy replaced.
//   - One whose change wasn't made is abandoned, and the refs it made are
//     removed.
//   - The branch's newest applied checkpoint, not restored, whose branch is
//     back at its Before, is restored, and a rebase's base goes back with
//     it: a restore that stopped before recording itself, or the same
//     change made by hand.
func (e *Engine) settleHistory(ctx context.Context, branch model.Branch) error {
	var checkpoints []model.Checkpoint
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		checkpoints, err = r.Checkpoints(branch.ID)
		return err
	}); err != nil {
		return err
	}
	var latest *model.Checkpoint
	pending := false
	for i, checkpoint := range checkpoints {
		switch checkpoint.State {
		case model.CheckpointPrepared:
			pending = true
		case model.CheckpointApplied:
			latest = &checkpoints[i]
		}
	}
	if !pending && (latest == nil || latest.RestoredAt != nil) {
		return nil
	}
	ref, err := e.Repo.ReadRef(ctx, "refs/heads/"+branch.Name)
	if err != nil {
		return err
	}
	if !ref.Exists {
		return nil
	}
	head := ref.Object
	for _, checkpoint := range checkpoints {
		if checkpoint.State != model.CheckpointPrepared {
			continue
		}
		made := head == string(checkpoint.After)
		if !made {
			if made, err = e.Repo.IsAncestor(ctx, string(checkpoint.After), head); err != nil {
				return err
			}
		}
		if made {
			if err := e.finishIndex(ctx, branch, checkpoint, head); err != nil {
				return err
			}
			if err := e.settleCheckpoint(ctx, checkpoint, model.CheckpointApplied, fmt.Sprintf("recorded %s, whose change a stopped dockhand had made", checkpoint.Name())); err != nil {
				return err
			}
			checkpoint.State = model.CheckpointApplied
			latest = &checkpoint
			continue
		}
		if err := e.dropCheckpointRefs(ctx, checkpoint); err != nil {
			return err
		}
		if err := e.settleCheckpoint(ctx, checkpoint, model.CheckpointAbandoned, fmt.Sprintf("abandoned %s, whose change a stopped dockhand hadn't made", checkpoint.Name())); err != nil {
			return err
		}
	}
	if latest != nil && latest.RestoredAt == nil && head == string(latest.Before) && head != string(latest.After) {
		restored := e.now()
		latest.RestoredAt = &restored
		if _, err := e.recordRestore(ctx, *latest, fmt.Sprintf("recorded the restore of %s's history from %s, which a stopped dockhand had made", branch.Name, latest.Name())); err != nil {
			return err
		}
	}
	return nil
}

// finishIndex resets the index after a tidy whose change was made, when it
// still holds what the tidy replaced: the tidy would have reset it next.
func (e *Engine) finishIndex(ctx context.Context, branch model.Branch, checkpoint model.Checkpoint, head string) error {
	if checkpoint.Kind != model.CheckpointTidy || checkpoint.Index == "" || head != string(checkpoint.After) || branch.Worktree == "" {
		return nil
	}
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return err
	}
	index, err := worktree.IndexTree(ctx)
	if err != nil || index != string(checkpoint.Index) {
		return err
	}
	return worktree.ResetIndex(ctx)
}

// dropCheckpointRefs removes the refs a checkpoint whose change wasn't
// made may have left, where they are still as it made them.
func (e *Engine) dropCheckpointRefs(ctx context.Context, checkpoint model.Checkpoint) error {
	var changes []git.RefChange
	for _, name := range []string{checkpoint.Ref(), checkpoint.IndexRef()} {
		value, err := e.Repo.ReadRef(ctx, name)
		if err != nil {
			return err
		}
		if value.Exists && (name == checkpoint.IndexRef() || value.Object == string(checkpoint.Before)) {
			changes = append(changes, git.RefChange{Name: name, Expected: value})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return e.Repo.UpdateRefs(ctx, changes)
}

// unfinished says how a history change ended when its Git change was made
// and its record wasn't: the next of these commands on the branch
// finishes the record.
func unfinished(what string, err error) error {
	return fmt.Errorf("%s, but recording it failed: %w; the next dockhand tidy, rebase, or restore of this branch finishes the record", what, err)
}

// named reports whether a checkpoint name, tidy-3, is one of the kinds.
func named(kind string) bool {
	return slices.Contains([]string{string(model.CheckpointTidy), string(model.CheckpointRebase)}, kind)
}
