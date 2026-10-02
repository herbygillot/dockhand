package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/history"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
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
	directories, err := e.directoriesNamed(ctx, tree, name)
	if err != nil {
		return "", err
	}
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

// directoriesNamed are the directories of a tree's categories named for
// a port, category/<name>, that hold a Portfile.
func (e *Engine) directoriesNamed(ctx context.Context, tree, name string) ([]string, error) {
	entries, err := e.Repo.ReadTree(ctx, tree)
	if err != nil {
		return nil, err
	}
	var candidates []string
	for _, entry := range entries {
		if entry.Type == "tree" && macports.IsCategory(entry.Name) {
			candidates = append(candidates, entry.Name+"/"+name+"/Portfile")
		}
	}
	found, err := e.Repo.FileBlobs(ctx, tree, candidates)
	if err != nil {
		return nil, err
	}
	var directories []string
	for path := range found {
		directories = append(directories, strings.TrimSuffix(path, "/Portfile"))
	}
	slices.Sort(directories)
	return directories, nil
}

// Retry queues a run's exact request again: the same revision and plan,
// whatever the branch holds now, as the branch's one check (queue). Until
// per-target reuse lands, the whole plan is built again.
func (e *Engine) Retry(ctx context.Context, previous model.Run) (model.Run, error) {
	if !previous.State.Terminal() {
		return model.Run{}, fmt.Errorf("%s is still %s; dockhand wait %s follows it", previous.Name(), previous.State, previous.Name())
	}
	var run model.Run
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		var err error
		run, err = e.queue(tx, model.Run{Branch: previous.Branch, Revision: previous.Revision, Plan: previous.Plan, Origin: model.OriginPerson, BaselineOf: previous.BaselineOf}, "queued, retrying "+previous.Name())
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
	// OlderBuilds are the builds the rebased commits name in Generated-By
	// other than this one, which a rebase keeps as the commits had them.
	OlderBuilds []string
}

// Rebase moves a branch's commits onto freshly fetched master, in its
// worktree, keeping a checkpoint of the old history for restore. A branch
// with uncommitted edits is refused, and a rebase that conflicts is
// abandoned with the branch as it was.
func (e *Engine) Rebase(ctx context.Context, branch model.Branch) (Rebased, error) {
	var result Rebased
	err := e.history().With(ctx, branch, func(ctx context.Context) error {
		var err error
		result, err = e.rebase(ctx, branch)
		return err
	})
	return result, err
}

// rebase replays a branch's commits onto fresh master, then moves the
// branch and its checkout to them, holding the branch's history lock
// (withHistory). The commits are made before anything moves, so a
// conflict changes nothing, and the checkpoint knows the rebased head
// before the branch holds it.
func (e *Engine) rebase(ctx context.Context, branch model.Branch) (Rebased, error) {
	current, err := e.Branch(ctx, branch.ID)
	if err != nil {
		return Rebased{}, err
	}
	worktree, err := e.worktree(ctx, current)
	if err != nil {
		return Rebased{}, err
	}
	edited, err := worktree.TrackedChanges(ctx)
	if err != nil {
		return Rebased{}, err
	}
	if len(edited) > 0 {
		return Rebased{}, fmt.Errorf("%s has uncommitted edits to %s; commit them (dockhand tidy) or set them aside before rebasing", current.ShortName(), listPaths(edited))
	}
	// A branch whose pull request merged has nothing to rebase: skim's
	// replayed nothing onto master, and then said submit would replace
	// the merged pull request's commits (field testing, 2026-10-02). Its
	// pull request is read now, since the record may be older than the
	// merge; a read that fails leaves the replay below to tell.
	if pr := current.PullRequest; pr != nil && current.State == model.BranchOpen {
		if refreshed, err := e.refresh(ctx, current); err == nil && refreshed.Branch.ID != "" {
			current = refreshed.Branch
		}
	}
	if err := ended(current); err != nil {
		return Rebased{}, err
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return Rebased{}, err
	}
	result := Rebased{From: current.Base, To: master}
	head, _, err := worktree.Branch(ctx, current.Name)
	if err != nil {
		return Rebased{}, err
	}
	if base, err := worktree.MergeBase(ctx, head, string(master)); err != nil {
		return Rebased{}, err
	} else if base == string(master) {
		result.UpToDate = true
		return result, e.Store.Update(ctx, e.Repository, func(tx store.Tx) error { return e.history().SetBase(tx, current.ID, master, "") })
	}
	committer, err := worktree.Author(ctx)
	if err != nil {
		return Rebased{}, err
	}
	committer.When = e.now()
	rebased, err := worktree.Replay(ctx, string(master), string(current.Base), head, committer)
	if errors.Is(err, git.ErrRebaseConflict) {
		return Rebased{}, fmt.Errorf("%w; %s is as it was. Resolve it by hand with git rebase %s, or ask for help on the PR", err, current.ShortName(), short(master))
	}
	if err != nil {
		return Rebased{}, err
	}
	// Counted as replayed: a change master already has is dropped.
	replayed, err := worktree.History(ctx, string(master), rebased)
	if err != nil {
		return Rebased{}, err
	}
	if len(replayed) == 0 {
		// Master has every change the branch makes: it was merged, by
		// its pull request or another's, and moving it onto master would
		// leave a branch that changes nothing.
		return Rebased{}, fmt.Errorf("master %s already has every change %s makes, so there's nothing to rebase; dockhand clean %s removes it once its pull request is merged", short(master), current.ShortName(), current.ShortName())
	}
	result.Commits = len(replayed)
	var messages []string
	for _, commit := range replayed {
		messages = append(messages, commit.Message)
	}
	result.OlderBuilds = commitmsg.OtherBuilds(messages)

	// The checkpoint is recorded before anything moves. Its ref keeps the
	// old history reachable before the branch leaves it, and the checkout
	// moves with the branch, as git reset --keep moves them.
	checkpoint := model.Checkpoint{Kind: model.CheckpointRebase, Branch: current.ID, Before: model.ObjectID(head), After: model.ObjectID(rebased), BaseBefore: current.Base, BaseAfter: master, At: e.now()}
	if err := e.history().Prepare(ctx, &checkpoint); err != nil {
		return Rebased{}, err
	}
	if err := e.history().Step("prepared"); err != nil {
		return Rebased{}, err
	}
	kept := git.RefChange{Name: checkpoint.Ref(), Desired: git.RefValue{Exists: true, Object: head}}
	if err := worktree.UpdateRefs(ctx, []git.RefChange{kept}); err != nil {
		return Rebased{}, errors.Join(err, e.history().Settle(ctx, checkpoint, model.CheckpointAbandoned, ""))
	}
	if err := worktree.MoveCheckout(ctx, head, rebased); err != nil {
		undone := worktree.UpdateRefs(context.WithoutCancel(ctx), []git.RefChange{{Name: kept.Name, Expected: kept.Desired}})
		return Rebased{}, errors.Join(fmt.Errorf("%s is as it was: %w", current.ShortName(), err), undone, e.history().Settle(ctx, checkpoint, model.CheckpointAbandoned, ""))
	}
	checkpoint.State = model.CheckpointApplied
	result.Checkpoint = &checkpoint
	if err := e.history().Step("moved"); err != nil {
		return result, err
	}
	if err := e.history().Settle(ctx, checkpoint, model.CheckpointApplied, ""); err != nil {
		return result, history.Unfinished("the rebase is done", err)
	}
	return result, nil
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

// ended refuses a branch whose pull request merged or closed: there's
// nothing to rebase, and submitting it again would reopen nothing.
func ended(branch model.Branch) error {
	number := 0
	if branch.PullRequest != nil {
		number = branch.PullRequest.Number
	}
	switch branch.State {
	case model.BranchMerged:
		return fmt.Errorf("#%d merged, so %s has nothing to rebase; dockhand clean %s removes what it leaves", number, branch.ShortName(), branch.ShortName())
	case model.BranchClosed:
		return fmt.Errorf("#%d was closed without merging, so %s has nothing to rebase; dockhand archive %s sets it aside", number, branch.ShortName(), branch.ShortName())
	}
	return nil
}
