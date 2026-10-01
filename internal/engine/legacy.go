package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Branches from before v3: earlier dockhand named its branches
// dockhand/bump/<port>-<id>, and no record here tracks them, so nothing
// reported or cleaned them; the hugo exercise's checkout held 22 (the
// cleanup, finding 3). clean --legacy sorts them by what master has of
// each.

// legacyBranches is where earlier dockhand's branches are.
const legacyBranches = "refs/heads/dockhand/bump/"

// LegacyKind is what master has of a branch from before v3.
type LegacyKind string

const (
	// LegacyOnMaster is a branch whose every commit master has, by
	// patch-id, as MacPorts' rebase merges leave one: removable, with your
	// fork's branch where it holds the same commit.
	LegacyOnMaster LegacyKind = "on-master"
	// LegacySuperseded is one whose port master declares at another
	// version than the branch started from: removable once a person has
	// looked.
	LegacySuperseded LegacyKind = "superseded"
	// LegacyUnfinished is the rest, which adopt takes up and nothing
	// removes.
	LegacyUnfinished LegacyKind = "unfinished"
)

// LegacyBranch is a branch from before v3, as master has it.
type LegacyBranch struct {
	Name, Head string
	Kind       LegacyKind
	// Detail says what master has of it.
	Detail string
	// Fork is your fork's branch of the same name, owner/repository:branch,
	// where it holds the branch's commit, which goes with it; ForkKept says
	// why it stays where it's there and doesn't.
	Fork, ForkKept string
	// Kept says why a branch master has stays; Done that it was removed.
	Kept string
	Done bool

	forkRemote string
}

// LegacyBranchNames are the branches from before v3 that no record here
// tracks, by name.
func (e *Engine) LegacyBranchNames(ctx context.Context) ([]string, error) {
	refs, err := e.Repo.ReadRefs(ctx, legacyBranches)
	if err != nil {
		return nil, err
	}
	var names []string
	err = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		for ref := range refs {
			name := strings.TrimPrefix(ref, "refs/heads/")
			_, err := r.BranchNamed(name)
			switch {
			case errors.Is(err, store.ErrNotFound):
				names = append(names, name)
			case err != nil:
				return err
			}
		}
		return nil
	})
	slices.Sort(names)
	return names, err
}

// PlanLegacy sorts the branches from before v3 by what master, fetched now,
// has of each (LegacyKind). Your fork's branch of the same name is read
// where your fork can be found.
func (e *Engine) PlanLegacy(ctx context.Context) ([]LegacyBranch, error) {
	names, err := e.LegacyBranchNames(ctx)
	if err != nil || len(names) == 0 {
		return nil, err
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return nil, err
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(master)})
	if err != nil {
		return nil, err
	}
	fork, forkErr := e.Fork(ctx, "")
	var plans []LegacyBranch
	for _, name := range names {
		head, _, err := e.Repo.Branch(ctx, name)
		if err != nil {
			return nil, err
		}
		branch := LegacyBranch{Name: name, Head: head}
		equivalent, own, err := e.Repo.Cherry(ctx, string(master), head)
		if err != nil {
			return nil, err
		}
		if own > 0 {
			branch.Kind, branch.Detail = LegacyUnfinished, plural(own, "commit")+" master hasn't"
			if detail, moved, err := e.legacySuperseded(ctx, string(master), trees[string(master)], head); err != nil {
				return nil, err
			} else if moved {
				branch.Kind, branch.Detail = LegacySuperseded, detail
			}
			plans = append(plans, branch)
			continue
		}
		branch.Kind, branch.Detail = LegacyOnMaster, "it has nothing master lacks"
		if equivalent > 0 {
			branch.Detail = "master has its " + plural(equivalent, "commit") + ", by their changes"
		}
		if branch.Kept, err = e.checkedOut(ctx, name, ""); err != nil {
			return nil, err
		}
		if forkErr == nil {
			value, err := e.Repo.RemoteHead(ctx, fork.PushURL, name)
			switch {
			case err != nil:
				branch.ForkKept = "it could not be read: " + err.Error()
			case !value.Exists:
			case value.Object == head:
				branch.Fork, branch.forkRemote = fork.Repository+":"+name, fork.PushURL
			default:
				branch.ForkKept = fork.Repository + ":" + name + " holds another commit"
			}
		}
		plans = append(plans, branch)
	}
	return plans, nil
}

// legacySuperseded says the ports a branch changes that master declares at
// another version than the branch started from, which a newer update in
// master may have superseded. The versions are those the Portfiles declare
// as literals (portfile.DeclaredVersion); one that isn't says nothing.
func (e *Engine) legacySuperseded(ctx context.Context, master, masterTree, head string) (string, bool, error) {
	base, err := e.Repo.MergeBase(ctx, head, master)
	if err != nil {
		return "", false, err
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{base, head})
	if err != nil {
		return "", false, err
	}
	changed, err := e.Repo.ChangedPaths(ctx, trees[base], trees[head])
	if err != nil {
		return "", false, err
	}
	declared := func(tree, directory string) (string, bool) {
		_, data, err := e.Repo.File(ctx, tree, directory+"/Portfile")
		if err != nil || data == nil {
			return "", false
		}
		return portfile.DeclaredVersion(data, "")
	}
	var moved []string
	for _, directory := range ScopeOf(changed).Ports {
		was, ok := declared(trees[base], directory)
		now, okNow := declared(masterTree, directory)
		if !ok || !okNow || now == was {
			continue
		}
		mine, _ := declared(trees[head], directory)
		moved = append(moved, fmt.Sprintf("%s at %s, where the branch took %s to %s", path.Base(directory), now, was, mine))
	}
	return "master has " + strings.Join(moved, "; "), len(moved) > 0, nil
}

// RemoveLegacy removes the branches from before v3 that master has, each
// with your fork's branch where it held the same commit, and each only
// while it still holds the commit planned; the rest are only said.
func (e *Engine) RemoveLegacy(ctx context.Context, plans []LegacyBranch) ([]LegacyBranch, error) {
	var removed []string
	for i := range plans {
		branch := &plans[i]
		if branch.Kind != LegacyOnMaster || branch.Kept != "" {
			continue
		}
		var conflict *git.RefConflict
		err := e.Repo.DeleteBranch(ctx, branch.Name, branch.Head)
		switch {
		case errors.As(err, &conflict):
			branch.Kept = "it moved while clean ran"
			continue
		case err != nil:
			return plans, fmt.Errorf("removing %s: %w", branch.Name, err)
		}
		branch.Done = true
		removed = append(removed, branch.Name)
		if branch.forkRemote == "" {
			continue
		}
		_, name, _ := strings.Cut(branch.Fork, ":")
		err = e.Repo.DeleteRemoteBranch(ctx, branch.forkRemote, name, git.RefValue{Exists: true, Object: branch.Head})
		switch {
		case errors.As(err, &conflict):
			branch.ForkKept, branch.Fork = branch.Fork+" moved while clean ran", ""
		case err != nil:
			return plans, fmt.Errorf("removing %s: %w", branch.Fork, err)
		default:
			removed = append(removed, branch.Fork)
		}
	}
	if len(removed) == 0 {
		return plans, nil
	}
	return plans, e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		_, err := tx.AppendEvent(model.Event{At: e.now(), Kind: "legacy.clean", Level: model.LevelInfo, Message: "removed " + strings.Join(removed, ", ")})
		return err
	})
}
