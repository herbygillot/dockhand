package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// CleanStep is one thing clean would remove, or keeps and why.
type CleanStep struct {
	// What names it: "worktree ~/…", "branch dockhand/jq-4k2p",
	// "ada/macports-ports:dockhand/jq-4k2p".
	What string
	// Kept says why it stays; empty when it would be removed.
	Kept string
	// Done is true once it was removed.
	Done bool

	kind     string
	path     string
	remote   string
	expected string
}

// CleanBranch is what clean would do for one merged branch.
type CleanBranch struct {
	Branch model.Branch
	// Merged is the commit the pull request was merged at.
	Merged model.ObjectID
	Steps  []CleanStep
}

// PlanClean previews removing what merged branches leave behind (Design
// v3 §6.13): the worktree, the local branch, and the fork's branch, each
// only while it still holds the merged commit. A dirty worktree, and work
// that went on past the merge, are kept. The branch's record stays, so it
// is still searchable.
func (e *Engine) PlanClean(ctx context.Context) ([]CleanBranch, error) {
	var merged []model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		merged, err = r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchMerged}})
		return err
	}); err != nil {
		return nil, err
	}
	var plans []CleanBranch
	for _, branch := range merged {
		plan, err := e.planCleanBranch(ctx, branch)
		if err != nil {
			return nil, err
		}
		if len(plan.Steps) > 0 {
			plans = append(plans, plan)
		}
	}
	return plans, nil
}

func (e *Engine) planCleanBranch(ctx context.Context, branch model.Branch) (CleanBranch, error) {
	plan := CleanBranch{Branch: branch}
	if pr := branch.PullRequest; pr != nil {
		plan.Merged = pr.Pushed
		if pr.Observed != nil && pr.Observed.Head != "" {
			plan.Merged = pr.Observed.Head
		}
	}
	head, _, err := e.Repo.Branch(ctx, branch.Name)
	hasBranch := err == nil
	if err != nil && !errors.Is(err, git.ErrBranchMissing) {
		return plan, err
	}
	beyond := ""
	if hasBranch && string(plan.Merged) != head {
		beyond = "it has commits beyond what was merged"
		if plan.Merged == "" {
			beyond = "the merged commit is not known"
		}
	}

	if branch.Managed && exists(branch.Worktree) {
		step := CleanStep{What: "worktree " + branch.Worktree, kind: "worktree", path: branch.Worktree}
		if beyond != "" {
			step.Kept = beyond
		} else if dirty, err := e.dirty(ctx, branch.Worktree); err != nil {
			return plan, err
		} else if dirty != "" {
			step.Kept = dirty
		}
		plan.Steps = append(plan.Steps, step)
	}
	if hasBranch {
		step := CleanStep{What: "branch " + branch.Name, kind: "branch", expected: head}
		switch {
		case beyond != "":
			step.Kept = beyond
		case !branch.Managed && branch.Worktree != "":
			if current, err := e.Repo.CurrentBranch(ctx); err == nil && current == branch.Name {
				step.Kept = "it is checked out in your checkout; switch away first"
			}
		}
		plan.Steps = append(plan.Steps, step)
	}
	if pr := branch.PullRequest; pr != nil && pr.Head != "" && plan.Merged != "" {
		repository, name, _ := strings.Cut(pr.Head, ":")
		step := CleanStep{What: pr.Head, kind: "fork", expected: string(plan.Merged)}
		remote, err := e.remoteFor(ctx, repository)
		switch {
		case err != nil:
			step.Kept = err.Error()
		default:
			step.remote = remote
			value, err := e.Repo.RemoteHead(ctx, remote, name)
			switch {
			case err != nil:
				step.Kept = "it could not be read: " + err.Error()
			case !value.Exists:
				step = CleanStep{}
			case value.Object != string(plan.Merged):
				step.Kept = "it has moved since the merge"
			}
		}
		if step.What != "" {
			plan.Steps = append(plan.Steps, step)
		}
	}
	return plan, nil
}

// dirty says why a worktree holds work of its own: uncommitted edits to
// tracked files, or untracked files.
func (e *Engine) dirty(ctx context.Context, directory string) (string, error) {
	worktree, err := git.Open(ctx, directory, e.options.Git)
	if err != nil {
		return "", err
	}
	edited, err := worktree.TrackedChanges(ctx)
	if err != nil {
		return "", err
	}
	if len(edited) > 0 {
		return "it has uncommitted edits to " + listPaths(edited), nil
	}
	untracked, err := worktree.Untracked(ctx)
	if err != nil {
		return "", err
	}
	if len(untracked) > 0 {
		return "it has untracked files: " + listPaths(untracked), nil
	}
	return "", nil
}

// remoteFor is the push URL of the Git remote for a GitHub repository.
func (e *Engine) remoteFor(ctx context.Context, repository string) (string, error) {
	remotes, err := e.Repo.Remotes(ctx)
	if err != nil {
		return "", err
	}
	for _, remote := range remotes {
		if name, err := e.forge().NameFromRemote(remote.PushURL); err == nil && strings.EqualFold(name, repository) {
			return remote.PushURL, nil
		}
	}
	return "", fmt.Errorf("no Git remote pushes to %s", repository)
}

// ApplyClean removes what the plans would, checking each again as it goes,
// and marks each step done. Steps that are kept are left alone.
func (e *Engine) ApplyClean(ctx context.Context, plans []CleanBranch) ([]CleanBranch, error) {
	for i := range plans {
		plan := &plans[i]
		for j := range plan.Steps {
			step := &plan.Steps[j]
			if step.Kept != "" {
				continue
			}
			var err error
			switch step.kind {
			case "worktree":
				if dirty, dirtyErr := e.dirty(ctx, step.path); dirtyErr != nil || dirty != "" {
					step.Kept = dirty
					err = dirtyErr
					break
				}
				err = e.Repo.RemoveWorktree(ctx, step.path)
				if err == nil {
					_ = os.Remove(step.path)
				}
			case "branch":
				err = e.Repo.DeleteBranch(ctx, plan.Branch.Name, step.expected)
			case "fork":
				_, name, _ := strings.Cut(plan.Branch.PullRequest.Head, ":")
				err = e.Repo.DeleteRemoteBranch(ctx, step.remote, name, git.RefValue{Exists: true, Object: step.expected})
			}
			if err != nil {
				var conflict *git.RefConflict
				if errors.As(err, &conflict) {
					step.Kept = "it moved while clean ran"
					continue
				}
				return plans, fmt.Errorf("removing %s: %w", step.What, err)
			}
			step.Done = step.Kept == ""
		}
		var removed []string
		everything := true
		for _, step := range plan.Steps {
			if step.Done {
				removed = append(removed, step.What)
			}
			everything = everything && step.Done
		}
		// Checkpoints and snapshots go only with everything else: kept work
		// may still want a restore.
		if everything {
			if err := e.dropRefs(ctx, plan.Branch); err != nil {
				return plans, err
			}
		}
		if len(removed) > 0 {
			if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
				_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: plan.Branch.ID, Kind: "branch.clean", Level: model.LevelInfo,
					Message: "removed " + strings.Join(removed, ", ")})
				return err
			}); err != nil {
				return plans, err
			}
		}
	}
	return plans, nil
}

// dropRefs removes the refs dockhand kept for a merged branch: its
// checkpoints and its snapshots' commits.
func (e *Engine) dropRefs(ctx context.Context, branch model.Branch) error {
	var names []string
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		checkpoints, err := r.Checkpoints(branch.ID)
		if err != nil {
			return err
		}
		for _, checkpoint := range checkpoints {
			names = append(names, checkpoint.Ref())
		}
		revisions, err := r.Revisions(branch.ID)
		for _, revision := range revisions {
			names = append(names, "refs/dockhand/revisions/"+string(revision.ID))
		}
		return err
	}); err != nil {
		return err
	}
	var changes []git.RefChange
	for _, name := range names {
		value, err := e.Repo.ReadRef(ctx, name)
		if err != nil {
			return err
		}
		if value.Exists {
			changes = append(changes, git.RefChange{Name: name, Expected: value})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return e.Repo.UpdateRefs(ctx, changes)
}
