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
// reported or cleaned them; the hugo exercise's checkout held 22, and its
// fork two more (the cleanup, finding 3). clean --legacy sorts them by
// what master has of each.

// legacyPrefix is how earlier dockhand named its branches.
const legacyPrefix = "dockhand/bump/"

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
	// Detail says what master has of it, or what it does that master
	// doesn't.
	Detail string
	// ForkOnly is a branch your fork has and this checkout doesn't.
	ForkOnly bool
	// Fork is your fork's branch of the same name, owner/repository:branch,
	// where it holds the branch's commit, which goes with it; ForkKept says
	// why it stays where it's there and doesn't.
	Fork, ForkKept string
	// Kept says why a branch master has stays; Done that it was removed.
	Kept string
	Done bool

	forkRemote string
}

// LegacyBranchNames are the branches from before v3 in this checkout that
// no record here tracks, by name.
func (e *Engine) LegacyBranchNames(ctx context.Context) ([]string, error) {
	refs, err := e.Repo.ReadRefs(ctx, "refs/heads/"+legacyPrefix)
	if err != nil {
		return nil, err
	}
	var names []string
	for ref := range refs {
		names = append(names, strings.TrimPrefix(ref, "refs/heads/"))
	}
	return e.untracked(ctx, names)
}

// untracked are the names no record here tracks, in order.
func (e *Engine) untracked(ctx context.Context, names []string) ([]string, error) {
	var found []string
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		for _, name := range names {
			_, err := r.BranchNamed(name)
			switch {
			case errors.Is(err, store.ErrNotFound):
				found = append(found, name)
			case err != nil:
				return err
			}
		}
		return nil
	})
	slices.Sort(found)
	return found, err
}

// PlanLegacy sorts the branches from before v3 by what master, fetched now,
// has of each (LegacyKind): this checkout's, and those your fork has and
// it doesn't, where your fork can be found, each fetched to be read.
func (e *Engine) PlanLegacy(ctx context.Context) ([]LegacyBranch, error) {
	local, err := e.LegacyBranchNames(ctx)
	if err != nil {
		return nil, err
	}
	fork, forkErr := e.Fork(ctx, "")
	forkHeads := map[string]string{}
	if forkErr == nil {
		if forkHeads, err = e.Repo.RemoteBranches(ctx, fork.PushURL, legacyPrefix); err != nil {
			return nil, err
		}
	}
	var forkOnly []string
	for name := range forkHeads {
		if !slices.Contains(local, name) {
			forkOnly = append(forkOnly, name)
		}
	}
	if forkOnly, err = e.untracked(ctx, forkOnly); err != nil {
		return nil, err
	}
	if len(local)+len(forkOnly) == 0 {
		return nil, nil
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return nil, err
	}
	var plans []LegacyBranch
	for _, name := range slices.Sorted(slices.Values(append(slices.Clone(local), forkOnly...))) {
		branch := LegacyBranch{Name: name, ForkOnly: slices.Contains(forkOnly, name)}
		if branch.ForkOnly {
			if branch.Head, _, err = e.Repo.FetchBranch(ctx, fork.PushURL, name); err != nil {
				return nil, fmt.Errorf("reading %s:%s: %w", fork.Repository, name, err)
			}
			branch.Fork, branch.forkRemote = fork.Repository+":"+name, fork.PushURL
		} else if branch.Head, _, err = e.Repo.Branch(ctx, name); err != nil {
			return nil, err
		}
		if err := e.sortLegacy(ctx, &branch, string(master)); err != nil {
			return nil, err
		}
		if branch.Kind == LegacyOnMaster && !branch.ForkOnly {
			if branch.Kept, err = e.checkedOut(ctx, name, ""); err != nil {
				return nil, err
			}
			if head, ok := forkHeads[name]; ok {
				if head == branch.Head {
					branch.Fork, branch.forkRemote = fork.Repository+":"+name, fork.PushURL
				} else {
					branch.ForkKept = fork.Repository + ":" + name + " holds another commit"
				}
			}
		}
		plans = append(plans, branch)
	}
	return plans, nil
}

// sortLegacy says what master has of a branch: every commit's change, by
// patch-id, named by its subject; its ports at other versions than the
// branch started from; or what the branch does that master doesn't.
func (e *Engine) sortLegacy(ctx context.Context, branch *LegacyBranch, master string) error {
	commits, err := e.Repo.Cherry(ctx, master, branch.Head)
	if err != nil {
		return err
	}
	own := slices.DeleteFunc(slices.Clone(commits), func(c git.CherryCommit) bool { return c.Equivalent })
	if len(own) == 0 {
		branch.Kind = LegacyOnMaster
		switch len(commits) {
		case 0:
			branch.Detail = "it has nothing master lacks"
		case 1:
			branch.Detail = fmt.Sprintf("master has the same change as its %q", commits[0].Subject)
		default:
			branch.Detail = fmt.Sprintf("master has the same changes as its %d commits, by patch", len(commits))
		}
		return nil
	}
	moves, err := e.portMoves(ctx, master, branch.Head)
	if err != nil {
		return err
	}
	var takes []string
	for _, move := range moves {
		if move.master == move.was && move.mine != move.was {
			takes = append(takes, fmt.Sprintf("%s from %s to %s, which master still has at %s", move.port, move.was, move.mine, move.was))
		}
	}
	switch {
	case superseded(moves) != "":
		branch.Kind, branch.Detail = LegacySuperseded, superseded(moves)
	case len(takes) > 0:
		branch.Kind, branch.Detail = LegacyUnfinished, "takes "+strings.Join(takes, "; ")
	default:
		branch.Kind, branch.Detail = LegacyUnfinished, plural(len(own), "commit")+" master hasn't"
	}
	return nil
}

// superseded says the ports master declares at other versions than a
// branch started from, as a newer update in master leaves them; empty
// where it has none.
func superseded(moves []portMove) string {
	var moved []string
	for _, move := range moves {
		if move.master != move.was {
			moved = append(moved, fmt.Sprintf("%s at %s, where the branch took %s to %s", move.port, move.master, move.was, move.mine))
		}
	}
	if len(moved) == 0 {
		return ""
	}
	return "master has " + strings.Join(moved, "; ")
}

// portMove is a port a branch changes, with the version its Portfile
// declares where the branch started, in the branch, and on master.
type portMove struct {
	port, was, mine, master string
}

// portMoves are the ports a branch changes whose Portfiles declare their
// versions as literals (portfile.DeclaredVersion), where it started and on
// master; one that doesn't says nothing.
func (e *Engine) portMoves(ctx context.Context, master, head string) ([]portMove, error) {
	base, err := e.Repo.MergeBase(ctx, head, master)
	if err != nil {
		return nil, err
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{base, head, master})
	if err != nil {
		return nil, err
	}
	changed, err := e.Repo.ChangedPaths(ctx, trees[base], trees[head])
	if err != nil {
		return nil, err
	}
	declared := func(tree, directory string) (string, bool) {
		_, data, err := e.Repo.File(ctx, tree, directory+"/Portfile")
		if err != nil || data == nil {
			return "", false
		}
		return portfile.DeclaredVersion(data, "")
	}
	var moves []portMove
	for _, directory := range ScopeOf(changed).Ports {
		was, ok := declared(trees[base], directory)
		now, okNow := declared(trees[master], directory)
		if !ok || !okNow {
			continue
		}
		mine, _ := declared(trees[head], directory)
		moves = append(moves, portMove{port: path.Base(directory), was: was, mine: mine, master: now})
	}
	return moves, nil
}

// RemoveLegacy removes the branches from before v3 that master has, each
// with your fork's branch where it held the same commit, a branch only on
// your fork from there alone, and each only while it still holds the
// commit planned; the rest are only said.
func (e *Engine) RemoveLegacy(ctx context.Context, plans []LegacyBranch) ([]LegacyBranch, error) {
	var removed []string
	var conflict *git.RefConflict
	for i := range plans {
		branch := &plans[i]
		if branch.Kind != LegacyOnMaster || branch.Kept != "" {
			continue
		}
		if !branch.ForkOnly {
			err := e.Repo.DeleteBranch(ctx, branch.Name, branch.Head)
			switch {
			case errors.As(err, &conflict):
				branch.Kept = "it moved while clean ran"
				continue
			case err != nil:
				return plans, fmt.Errorf("removing %s: %w", branch.Name, err)
			}
			removed = append(removed, branch.Name)
		}
		branch.Done = true
		if branch.forkRemote == "" {
			continue
		}
		_, name, _ := strings.Cut(branch.Fork, ":")
		err := e.Repo.DeleteRemoteBranch(ctx, branch.forkRemote, name, git.RefValue{Exists: true, Object: branch.Head})
		switch {
		case errors.As(err, &conflict):
			branch.ForkKept, branch.Fork = branch.Fork+" moved while clean ran", ""
			branch.Done = !branch.ForkOnly
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
