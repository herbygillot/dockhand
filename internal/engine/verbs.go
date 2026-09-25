package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Edit readies a port's directory in the branch's worktree for editing:
// found by name, and added to a sparse worktree's cone. It returns the
// port's directory.
func (e *Engine) Edit(ctx context.Context, branch model.Branch, port string) (string, error) {
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return "", err
	}
	head, err := worktree.Resolve(ctx, "HEAD^{commit}")
	if err != nil {
		return "", err
	}
	trees, err := worktree.CommitTrees(ctx, []string{head})
	if err != nil {
		return "", err
	}
	directory, err := e.findDirectory(ctx, trees[head], port)
	if err != nil {
		return "", err
	}
	if err := expandFor(ctx, worktree, []string{directory + "/Portfile"}); err != nil {
		return "", err
	}
	return directory, nil
}

// findDirectory is the directory of a port by name: category/<name> when a
// category holds one, else the port reader's answer, which knows subports.
func (e *Engine) findDirectory(ctx context.Context, tree, name string) (string, error) {
	if strings.Contains(name, "/") {
		return strings.TrimSuffix(name, "/Portfile"), nil
	}
	entries, err := e.Repo.ReadTree(ctx, tree)
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, entry := range entries {
		if entry.Type == "tree" && !strings.HasPrefix(entry.Name, "_") && !strings.HasPrefix(entry.Name, ".") {
			candidates = append(candidates, entry.Name+"/"+name+"/Portfile")
		}
	}
	found, err := e.Repo.FileBlobs(ctx, tree, candidates)
	if err != nil {
		return "", err
	}
	var directories []string
	for path := range found {
		directories = append(directories, strings.TrimSuffix(path, "/Portfile"))
	}
	slices.Sort(directories)
	switch len(directories) {
	case 1:
		return directories[0], nil
	case 0:
	default:
		return "", fmt.Errorf("%s is in several directories: %s; name one", name, strings.Join(directories, ", "))
	}
	reader, err := e.portReader()
	if err != nil {
		return "", err
	}
	directory, err := reader.Directory(ctx, model.Source{Tree: model.ObjectID(tree)}, name)
	if err != nil {
		return "", fmt.Errorf("no port %s in this tree: %w", name, err)
	}
	return directory, nil
}

// Retry queues a run's exact request again: the same revision and plan,
// whatever the branch holds now. Until per-target reuse lands, the whole
// plan is built again.
func (e *Engine) Retry(ctx context.Context, previous model.Run) (model.Run, error) {
	if !previous.State.Terminal() {
		return model.Run{}, fmt.Errorf("%s is still %s; dockhand wait %s follows it", previous.Name(), previous.State, previous.Name())
	}
	var run model.Run
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		number, err := tx.NextRunNumber()
		if err != nil {
			return err
		}
		run = model.Run{ID: model.RunID(store.NewID("run")), Branch: previous.Branch, Revision: previous.Revision, Plan: previous.Plan, Number: number, Origin: model.OriginPerson, State: model.RunQueued, CreatedAt: e.now()}
		if err := tx.AddRun(run); err != nil {
			return err
		}
		_, err = tx.AppendEvent(model.Event{At: run.CreatedAt, Branch: run.Branch, Run: run.ID, Kind: "run.state", Level: model.LevelInfo,
			Message: fmt.Sprintf("%s queued, retrying %s", run.Name(), previous.Name())})
		return err
	})
	return run, err
}

// Rebased is what a rebase did.
type Rebased struct {
	From, To model.ObjectID
	// UpToDate is true when the branch already started from master.
	UpToDate   bool
	Checkpoint *model.Checkpoint
	Commits    int
}

// Rebase moves a branch's commits onto freshly fetched master, in its
// worktree, keeping a checkpoint of the old history for restore. A branch
// with uncommitted edits is refused, and a rebase that conflicts is
// abandoned with the branch as it was.
func (e *Engine) Rebase(ctx context.Context, branch model.Branch) (Rebased, error) {
	worktree, err := e.worktree(ctx, branch)
	if err != nil {
		return Rebased{}, err
	}
	edited, err := worktree.TrackedChanges(ctx)
	if err != nil {
		return Rebased{}, err
	}
	if len(edited) > 0 {
		return Rebased{}, fmt.Errorf("%s has uncommitted edits to %s; commit them (dockhand tidy) or set them aside before rebasing", branch.ShortName(), listPaths(edited))
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return Rebased{}, err
	}
	result := Rebased{From: branch.Base, To: master}
	head, _, err := worktree.Branch(ctx, branch.Name)
	if err != nil {
		return Rebased{}, err
	}
	if base, err := worktree.MergeBase(ctx, head, string(master)); err != nil {
		return Rebased{}, err
	} else if base == string(master) {
		result.UpToDate = true
		return result, e.setBase(ctx, branch, master, "")
	}
	if result.Commits, err = worktree.CountCommits(ctx, string(branch.Base), head); err != nil {
		return Rebased{}, err
	}
	if err := worktree.Rebase(ctx, string(master), string(branch.Base), branch.Name); err != nil {
		if errors.Is(err, git.ErrRebaseConflict) {
			return Rebased{}, fmt.Errorf("%w; %s is as it was. Resolve it by hand with git rebase %s, or ask for help on the PR", err, branch.ShortName(), short(master))
		}
		return Rebased{}, err
	}
	rebased, _, err := worktree.Branch(ctx, branch.Name)
	if err != nil {
		return Rebased{}, err
	}
	checkpoint := model.Checkpoint{Kind: model.CheckpointRebase, Branch: branch.ID, Before: model.ObjectID(head), After: model.ObjectID(rebased), At: e.now()}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if checkpoint.Number, err = tx.NextCheckpointNumber(); err != nil {
			return err
		}
		if err := worktree.UpdateRefs(ctx, []git.RefChange{{Name: checkpoint.Ref(), Desired: git.RefValue{Exists: true, Object: head}}}); err != nil {
			return err
		}
		return tx.AddCheckpoint(checkpoint)
	})
	if err != nil {
		return Rebased{}, fmt.Errorf("the rebase is done, but its checkpoint was not recorded: %w", err)
	}
	result.Checkpoint = &checkpoint
	return result, e.setBase(ctx, branch, master, checkpoint.Name())
}

func (e *Engine) setBase(ctx context.Context, branch model.Branch, base model.ObjectID, checkpoint string) error {
	return e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		current, err := tx.Branch(branch.ID)
		if err != nil {
			return err
		}
		previous := current.Base
		current.Base = base
		if err := tx.UpdateBranch(current); err != nil {
			return err
		}
		message := fmt.Sprintf("rebased %s from master %s onto %s", branch.Name, short(previous), short(base))
		if checkpoint != "" {
			message += " (checkpoint " + checkpoint + ")"
		}
		_, err = tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: "branch.rebase", Level: model.LevelInfo, Message: message})
		return err
	})
}

// Archive hides a branch from status without touching its files, its Git
// branch, or its pull request; undo brings it back.
func (e *Engine) Archive(ctx context.Context, branch model.Branch, undo bool) (model.Branch, error) {
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		current, err := tx.Branch(branch.ID)
		if err != nil {
			return err
		}
		next, verb := model.BranchArchived, "archived"
		if undo {
			next, verb = model.BranchOpen, "brought back"
		}
		if current.State == next {
			branch = current
			return nil
		}
		if !current.State.CanBecome(next) {
			return fmt.Errorf("%s is %s, so it can't be %s", current.ShortName(), current.State, verb)
		}
		current.State = next
		if err := tx.UpdateBranch(current); err != nil {
			return err
		}
		branch = current
		_, err = tx.AppendEvent(model.Event{At: e.now(), Branch: branch.ID, Kind: "branch.state", Level: model.LevelInfo, Message: branch.Name + " " + verb})
		return err
	})
	return branch, err
}
