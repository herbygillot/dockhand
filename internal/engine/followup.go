package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// FollowUp is what start --from did: the branch it started, the merged
// branch it came from, and the commits it picked onto master.
type FollowUp struct {
	Branch model.Branch
	From   model.Branch
	Picked []git.HistoryCommit
}

// PastMerge are a merged branch's commits past the commit its pull
// request merged at, which went on after the merge and the merge left out,
// as wasmedge's follow-up did (the reflections' F): none for another
// branch, one whose merged commit isn't known, or one whose changes
// master has by another route (changesOnMaster), as a branch retidied
// after its submit has.
func (e *Engine) PastMerge(ctx context.Context, branch model.Branch, head string) ([]git.HistoryCommit, error) {
	merged := pushedHead(branch)
	if branch.State != model.BranchMerged || merged == "" || head == "" || head == string(merged) {
		return nil, nil
	}
	// A merged commit this clone doesn't have, as one GitHub made of
	// another's push, has nothing the branch went on from.
	if ancestor, err := e.Repo.IsAncestor(ctx, string(merged), head); err != nil || !ancestor {
		return nil, nil
	}
	if onMaster, err := e.changesOnMaster(ctx, branch, head); err != nil || onMaster {
		return nil, err
	}
	return e.Repo.History(ctx, string(merged), head)
}

// StartFrom starts a branch from master, fetched now, with a merged
// branch's commits past its merge picked onto it, as git rebase would
// replay them (Then's item 7): the follow-up a merge left, which was made
// by hand. A commit master has the change of is dropped; a conflict
// starts nothing.
func (e *Engine) StartFrom(ctx context.Context, name string, from model.Branch) (FollowUp, error) {
	result := FollowUp{From: from}
	if from.State != model.BranchMerged {
		return result, fmt.Errorf("start --from takes a merged branch's commits past its merge, and %s is %s; dockhand path %s works in it", from.ShortName(), from.State, from.ShortName())
	}
	head, _, err := e.Repo.Branch(ctx, from.Name)
	if errors.Is(err, git.ErrBranchMissing) {
		return result, fmt.Errorf("%s's Git branch is gone, so it has no commits past its merge to start from", from.ShortName())
	}
	if err != nil {
		return result, err
	}
	past, err := e.PastMerge(ctx, from, head)
	if err != nil {
		return result, err
	}
	if len(past) == 0 {
		return result, fmt.Errorf("%s has no commits past what #%d merged, so there's nothing to start from; dockhand clean %s removes it", from.ShortName(), from.PullRequest.Number, from.ShortName())
	}
	master, err := e.fetchMaster(ctx)
	if err != nil {
		return result, err
	}
	committer, err := e.Repo.Author(ctx)
	if err != nil {
		return result, err
	}
	committer.When = e.now()
	picked, err := e.Repo.Replay(ctx, string(master), string(pushedHead(from)), head, committer)
	if errors.Is(err, git.ErrRebaseConflict) {
		return result, fmt.Errorf("%w; nothing was started. Pick them by hand with git cherry-pick onto master %s", err, master.Short())
	}
	if err != nil {
		return result, err
	}
	if result.Picked, err = e.Repo.History(ctx, string(master), picked); err != nil {
		return result, err
	}
	if len(result.Picked) == 0 {
		return result, fmt.Errorf("master %s already has every change %s's commits past its merge make, so there's nothing to start from", master.Short(), from.ShortName())
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(master), picked})
	if err != nil {
		return result, err
	}
	paths, err := e.Repo.ChangedPaths(ctx, trees[string(master)], trees[picked])
	if err != nil {
		return result, err
	}
	result.Branch, err = e.Start(ctx, StartRequest{Name: name, Base: master, Head: model.ObjectID(picked), Directories: macports.ScopeOf(paths).Ports, Ports: from.Ports})
	return result, err
}
