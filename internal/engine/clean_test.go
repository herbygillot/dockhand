package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/record"
)

// mergedBranch submits an update and has GitHub merge it.
func mergedBranch(t *testing.T) (fixture, *Engine, *fakeForge, model.Branch) {
	t.Helper()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	fake.prs[submitted.PullRequest.Ref.Number].State = record.PullRequestMerged
	refreshed, err := e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, model.BranchMerged, refreshed[0].Branch.State)
	return f, e, fake, refreshed[0].Branch
}

func whats(plans []CleanBranch) map[string]string {
	all := map[string]string{}
	for _, plan := range plans {
		for _, step := range plan.Steps {
			all[step.What] = step.Kept
		}
	}
	return all
}

func TestCleanRemovesWhatAMergedBranchLeaves(t *testing.T) {
	_, e, fake, branch := mergedBranch(t)
	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"worktree " + branch.Worktree:           "",
		"branch dockhand/jq-update":             "",
		"ada/macports-ports:dockhand/jq-update": "",
	}, whats(plans))
	require.DirExists(t, branch.Worktree, "a plan removes nothing")

	done, err := e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	for _, step := range done[0].Steps {
		require.True(t, step.Done, step.What)
	}
	require.NoDirExists(t, branch.Worktree)
	require.Empty(t, fake.head("dockhand/jq-update"), "the fork's branch is gone")
	require.Empty(t, run(t, e.Repo.Root, "for-each-ref", "refs/dockhand/", "refs/heads/dockhand/"))

	again, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Empty(t, again, "nothing left")
	merged, err := e.Status(t.Context(), model.BranchMerged)
	require.NoError(t, err)
	require.Len(t, merged, 1, "the record stays")
}

func TestCleanKeepsWorkOfItsOwn(t *testing.T) {
	_, e, _, branch := mergedBranch(t)
	require.NoError(t, os.WriteFile(filepath.Join(branch.Worktree, "notes.txt"), []byte("mine\n"), 0o644))
	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, "it has untracked files: notes.txt", whats(plans)["worktree "+branch.Worktree])

	require.NoError(t, os.Remove(filepath.Join(branch.Worktree, "notes.txt")))
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.2\n"})
	run(t, branch.Worktree, "commit", "-q", "-am", "jq: update to 1.8.2")
	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	kept := whats(plans)
	require.Equal(t, "it has commits beyond what was merged", kept["worktree "+branch.Worktree])
	require.Equal(t, "it has commits beyond what was merged", kept["branch dockhand/jq-update"])
	done, err := e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.DirExists(t, branch.Worktree)
	require.NotEmpty(t, run(t, e.Repo.Root, "for-each-ref", "refs/dockhand/checkpoints/"), "checkpoints stay while work does")
	require.True(t, done[0].Steps[2].Done, "the fork's branch still held the merge, so it went")
}
