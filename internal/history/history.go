// Package history makes a change to a branch's history, a tidy, rebase, or
// restore, one complete transition (Design v3 §8). The engine decides what
// the change is; this package holds the branch's lock while it is made,
// records it around its Git change, and finishes what a process that
// stopped part-way left. A change never calls Git inside a transaction:
//
//   - a tidy or rebase computes its new commits, which nothing refers to
//     yet; records its checkpoint as prepared (Prepare); makes its Git
//     change, guarded by compare-and-swap; and settles the checkpoint as
//     applied, or as abandoned when the change couldn't be made (Settle);
//   - a restore makes its Git change, then records it (RecordRestore).
//
// A commit the store reports as uncertain is read back before anything
// more is done, and a Git change once made is never undone. A process
// that stops part-way leaves a state the next change on the branch
// recognizes, from what Git shows, and finishes (With).
package history

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/failpoint"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Transitions changes the branch histories of one registered repository.
type Transitions struct {
	Repo       *git.Repository
	Store      store.Store
	Repository model.RepositoryID
	Now        func() time.Time
	// Worktree opens a branch's checkout.
	Worktree func(ctx context.Context, branch model.Branch) (*git.Repository, error)
	// StopAt stops a change at a step, as if its process ended there:
	// tests set it (Step).
	StopAt func(step string) error
}

// With runs fn holding the branch's history lock, after finishing
// whatever a stopped process left.
func (h *Transitions) With(ctx context.Context, branch model.Branch, fn func(context.Context) error) error {
	return h.Repo.WithBranchLock(ctx, branch.Name, func(ctx context.Context) error {
		if err := h.settle(ctx, branch); err != nil {
			return fmt.Errorf("finishing what a stopped dockhand left of %s: %w", branch.ShortName(), err)
		}
		return fn(ctx)
	})
}

// Step is a point in a change where a test stops it, as if the process
// ended there; nil outside tests.
func (h *Transitions) Step(step string) error {
	if h.StopAt == nil {
		return nil
	}
	return h.StopAt(step)
}

// Change is a change of a branch's history that a checkpoint keeps, as
// Make makes it.
type Change struct {
	// Failpoint is the acceptance harness's kill step once the checkpoint
	// is prepared, "tidy.prepared".
	Failpoint string
	// Move makes the change, the refs and the checkout; where it fails, it
	// has undone what it did itself, and the checkpoint is abandoned.
	Move func(context.Context) error
	// After is what follows the move and can't undo it, with the event's
	// message; an error from it leaves the checkpoint for the next lock
	// to settle (settle).
	After func(context.Context) (string, error)
	// Made says what's done once it moved, for a settle that fails ("the
	// commits are made").
	Made string
}

// Make makes a change a checkpoint keeps, as tidy and rebase each wrote it
// out (the code-organization review's finding 26): the checkpoint
// recorded as prepared before anything moves, the change, the checkpoint
// abandoned where the change fails, and settled as applied, with its
// event, after. checkpoint's State is applied once the change has moved,
// whatever follows.
func (h *Transitions) Make(ctx context.Context, checkpoint *model.Checkpoint, change Change) error {
	if err := h.Prepare(ctx, checkpoint); err != nil {
		return err
	}
	failpoint.Hit(change.Failpoint)
	if err := h.Step("prepared"); err != nil {
		return err
	}
	if err := change.Move(ctx); err != nil {
		return errors.Join(err, h.Settle(ctx, *checkpoint, model.CheckpointAbandoned, ""))
	}
	checkpoint.State = model.CheckpointApplied
	if err := h.Step("moved"); err != nil {
		return err
	}
	message := ""
	if change.After != nil {
		var err error
		if message, err = change.After(ctx); err != nil {
			return err
		}
	}
	if err := h.Settle(ctx, *checkpoint, model.CheckpointApplied, message); err != nil {
		return Unfinished(change.Made, err)
	}
	return nil
}

// Prepare records a checkpoint as prepared, before the change it
// keeps is made, and gives it its number.
func (h *Transitions) Prepare(ctx context.Context, checkpoint *model.Checkpoint) error {
	checkpoint.State = model.CheckpointPrepared
	// A name another database gave on this checkout is skipped, so the
	// name people type stays tidy-N (rc2's full stage, a copy run on its
	// original's checkout).
	taken, err := takenNumber(ctx, h.Repo)
	if err != nil {
		return fmt.Errorf("reading the checkout's checkpoints failed, so nothing was changed: %w", err)
	}
	err = store.Recorded(ctx, h.Store, h.Repository, func(tx store.Tx) error {
		number, err := tx.NextCheckpointNumber()
		if err != nil {
			return err
		}
		checkpoint.Number = max(number, taken+1)
		return tx.AddCheckpoint(*checkpoint)
	}, checkpointRecorded(&checkpoint.Number, func(recorded model.Checkpoint) bool {
		return recorded.Branch == checkpoint.Branch && recorded.Before == checkpoint.Before && recorded.After == checkpoint.After
	}))
	if err != nil {
		return fmt.Errorf("recording the checkpoint failed, so nothing was changed: %w", err)
	}
	return nil
}

// Settle records a prepared checkpoint as applied or abandoned,
// with the event that says so. An applied rebase moves the branch's base
// in the same transaction.
func (h *Transitions) Settle(ctx context.Context, checkpoint model.Checkpoint, state model.CheckpointState, message string) error {
	checkpoint.State = state
	return store.Recorded(ctx, h.Store, h.Repository, func(tx store.Tx) error {
		if err := tx.SettleCheckpoint(checkpoint); err != nil {
			return err
		}
		if state == model.CheckpointApplied && checkpoint.Kind == model.CheckpointRebase {
			if err := h.SetBase(tx, checkpoint.Branch, checkpoint.BaseAfter, checkpoint.Name()); err != nil {
				return err
			}
		}
		if message == "" {
			return nil
		}
		_, err := tx.AppendEvent(model.Event{At: h.Now(), Branch: checkpoint.Branch, Kind: model.CheckpointEvent(checkpoint.Kind), Level: model.LevelInfo, Message: message})
		return err
	}, checkpointRecorded(&checkpoint.Number, func(recorded model.Checkpoint) bool { return recorded.State == state }))
}

// RecordRestore records a checkpoint's restore, and puts back the base the
// restored history starts from, in one transaction.
func (h *Transitions) RecordRestore(ctx context.Context, checkpoint model.Checkpoint, message string) (model.Branch, error) {
	var branch model.Branch
	// The branch is as the transaction wrote it, where its commit's
	// outcome was unknown and the restore is there all the same.
	err := store.Recorded(ctx, h.Store, h.Repository, func(tx store.Tx) error {
		if err := tx.MarkRestored(checkpoint); err != nil {
			return err
		}
		current, err := tx.Branch(checkpoint.Branch)
		if err != nil {
			return err
		}
		// The history put back starts from the master it started from
		// then, whatever the branch's base is now.
		if current.Base != checkpoint.BaseBefore {
			message += fmt.Sprintf(", back onto master %s", checkpoint.BaseBefore.Short())
			current.Base = checkpoint.BaseBefore
			if err := tx.UpdateBranch(current); err != nil {
				return err
			}
		}
		branch = current
		_, err = tx.AppendEvent(model.Event{At: *checkpoint.RestoredAt, Branch: checkpoint.Branch, Kind: model.EventBranchRestore, Level: model.LevelInfo, Message: message})
		return err
	}, checkpointRecorded(&checkpoint.Number, func(recorded model.Checkpoint) bool { return recorded.RestoredAt != nil }))
	return branch, err
}

// checkpointRecorded witnesses a checkpoint's record, as recorded is
// what was written (store.Recorded). A checkpoint numbered as it's
// written is read by the number the write gave it.
func checkpointRecorded(number *int, recorded func(model.Checkpoint) bool) func(store.Reader) bool {
	return func(r store.Reader) bool {
		checkpoint, err := r.Checkpoint(*number)
		return err == nil && recorded(checkpoint)
	}
}

// Read reads a checkpoint back, as it is recorded now.
func (h *Transitions) Read(ctx context.Context, number int) (model.Checkpoint, bool) {
	var checkpoint model.Checkpoint
	err := h.Store.View(ctx, h.Repository, func(r store.Reader) error {
		var err error
		checkpoint, err = r.Checkpoint(number)
		return err
	})
	return checkpoint, err == nil
}

// SetBase records a branch's new base, and the rebase that moved it; a
// base that is already the branch's changes nothing, and says nothing.
func (h *Transitions) SetBase(tx store.Tx, id model.BranchID, base model.ObjectID, checkpoint string) error {
	current, err := tx.Branch(id)
	if err != nil || current.Base == base {
		return err
	}
	previous := current.Base
	current.Base = base
	if err := tx.UpdateBranch(current); err != nil {
		return err
	}
	message := fmt.Sprintf("rebased %s from master %s onto %s", current.Name, previous.Short(), base.Short())
	if checkpoint != "" {
		message += " (checkpoint " + checkpoint + ")"
	}
	_, err = tx.AppendEvent(model.Event{At: h.Now(), Branch: id, Kind: model.CheckpointEvent(model.CheckpointRebase), Level: model.LevelInfo, Message: message})
	return err
}

// settle finishes what a process that stopped part-way through a
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
func (h *Transitions) settle(ctx context.Context, branch model.Branch) error {
	var checkpoints []model.Checkpoint
	if err := h.Store.View(ctx, h.Repository, func(r store.Reader) error {
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
	ref, err := h.Repo.ReadRef(ctx, "refs/heads/"+branch.Name)
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
			if made, err = h.Repo.IsAncestor(ctx, string(checkpoint.After), head); err != nil {
				return err
			}
		}
		if made {
			if err := h.finishIndex(ctx, branch, checkpoint, head); err != nil {
				return err
			}
			if err := h.Settle(ctx, checkpoint, model.CheckpointApplied, fmt.Sprintf("recorded %s, whose change a stopped dockhand had made", checkpoint.Name())); err != nil {
				return err
			}
			checkpoint.State = model.CheckpointApplied
			latest = &checkpoint
			continue
		}
		if err := h.dropCheckpointRefs(ctx, checkpoint); err != nil {
			return err
		}
		if err := h.Settle(ctx, checkpoint, model.CheckpointAbandoned, fmt.Sprintf("abandoned %s, whose change a stopped dockhand hadn't made", checkpoint.Name())); err != nil {
			return err
		}
	}
	if latest != nil && latest.RestoredAt == nil && head == string(latest.Before) && head != string(latest.After) {
		restored := h.Now()
		latest.RestoredAt = &restored
		if _, err := h.RecordRestore(ctx, *latest, fmt.Sprintf("recorded the restore of %s's history from %s, which a stopped dockhand had made", branch.Name, latest.Name())); err != nil {
			return err
		}
	}
	return nil
}

// finishIndex resets the index after a tidy whose change was made, when it
// still holds what the tidy replaced: the tidy would have reset it next.
func (h *Transitions) finishIndex(ctx context.Context, branch model.Branch, checkpoint model.Checkpoint, head string) error {
	if checkpoint.Kind != model.CheckpointTidy || checkpoint.Index == "" || head != string(checkpoint.After) || branch.Worktree == "" {
		return nil
	}
	worktree, err := h.Worktree(ctx, branch)
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
func (h *Transitions) dropCheckpointRefs(ctx context.Context, checkpoint model.Checkpoint) error {
	changes, err := KeptRefs(ctx, h.Repo, checkpoint)
	if err != nil {
		return err
	}
	return h.Repo.UpdateRefs(ctx, changes)
}

// KeptRefs are the deletions of a checkpoint's refs that hold what it
// kept: its ref at Before, and its index ref at a commit on Before. A
// ref of the checkpoint's name that holds anything else is another
// database's on this checkout (a copy run on its original's checkout,
// rc2's full stage), and stays.
func KeptRefs(ctx context.Context, repo *git.Repository, checkpoint model.Checkpoint) ([]git.RefChange, error) {
	var changes []git.RefChange
	value, err := repo.ReadRef(ctx, checkpoint.Ref())
	if err != nil {
		return nil, err
	}
	if value.Exists && value.Object == string(checkpoint.Before) {
		changes = append(changes, git.RefChange{Name: checkpoint.Ref(), Expected: value})
	}
	index, err := repo.ReadRef(ctx, checkpoint.IndexRef())
	if err != nil || !index.Exists {
		return changes, err
	}
	parents, err := repo.Parents(ctx, index.Object)
	if err != nil {
		return nil, err
	}
	if len(parents) == 1 && parents[0] == string(checkpoint.Before) {
		changes = append(changes, git.RefChange{Name: checkpoint.IndexRef(), Expected: index})
	}
	return changes, nil
}

// TakenName says a change's checkpoint couldn't take its name, which
// another process gave a checkpoint of its own on this checkout after
// Prepare read the names taken: nothing moved, and running again takes
// the next. It's nil where err is no such refusal.
func TakenName(checkpoint model.Checkpoint, err error) error {
	var conflict *git.RefConflict
	if errors.As(err, &conflict) && (conflict.Name == checkpoint.Ref() || conflict.Name == checkpoint.IndexRef()) {
		return fmt.Errorf("%s is another dockhand's checkpoint on this checkout, so nothing was changed; run again: %w", checkpoint.Name(), err)
	}
	return nil
}

// takenNumber is the highest checkpoint number named under
// refs/dockhand/checkpoints/ in the checkout, whichever database named it.
func takenNumber(ctx context.Context, repo *git.Repository) (int, error) {
	refs, err := repo.ReadRefs(ctx, "refs/dockhand/checkpoints/")
	if err != nil {
		return 0, err
	}
	highest := 0
	for name := range refs {
		name = strings.TrimSuffix(strings.TrimPrefix(name, "refs/dockhand/checkpoints/"), "-index")
		for _, kind := range []model.CheckpointKind{model.CheckpointTidy, model.CheckpointRebase} {
			if number, ok := strings.CutPrefix(name, string(kind)+"-"); ok {
				if n, err := strconv.Atoi(number); err == nil {
					highest = max(highest, n)
				}
			}
		}
	}
	return highest, nil
}

// Unfinished says how a history change ended when its Git change was made
// and its record wasn't: the next of these commands on the branch
// finishes the record.
func Unfinished(what string, err error) error {
	return fmt.Errorf("%s, but recording it failed: %w; the next dockhand tidy, rebase, or undo of this branch finishes the record", what, err)
}
