package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
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
	fake.prs[submitted.PullRequest.Ref.Number].State = forge.PullRequestMerged
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

func TestCleanupRemovesWhatCleanWouldAndOldIndexes(t *testing.T) {
	_, e, fake, branch := mergedBranch(t)
	require.NoError(t, os.WriteFile(filepath.Join(branch.Worktree, "notes.txt"), []byte("mine\n"), 0o644))
	cache := t.TempDir()
	t.Setenv("DOCKHAND_INDEX_CACHE", cache)
	profile := filepath.Join(cache, strings.Repeat("ab", 32))
	old, fresh := filepath.Join(profile, "generations", strings.Repeat("1", 40)), filepath.Join(profile, "generations", strings.Repeat("2", 40))
	for _, dir := range []string{old, fresh} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(profile, "cache.lock"), nil, 0o644))
	require.NoError(t, os.Chtimes(old, at.Add(-30*24*time.Hour), at.Add(-30*24*time.Hour)))
	require.NoError(t, os.Chtimes(fresh, at, at))

	report, err := e.Cleanup(t.Context(), session(t, e), 7*24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, []string{old}, report.Indexes)
	require.NoDirExists(t, old)
	require.DirExists(t, fresh)
	require.Equal(t, map[string]string{
		"worktree " + branch.Worktree:           "it has untracked files: notes.txt",
		"branch dockhand/jq-update":             "the worktree it is checked out in is kept",
		"ada/macports-ports:dockhand/jq-update": "",
	}, whats(report.Branches))
	require.Equal(t, 2, report.Removed(), "the fork's branch and the old index")
	require.Equal(t, "dockhand/jq-update", run(t, branch.Worktree, "branch", "--show-current"), "the kept worktree keeps its branch")
	require.FileExists(t, filepath.Join(branch.Worktree, "notes.txt"), "work of its own is kept")
	require.Empty(t, fake.head("dockhand/jq-update"))

	again, err := e.Cleanup(t.Context(), session(t, e), 7*24*time.Hour)
	require.NoError(t, err)
	require.Zero(t, again.Removed(), "what is kept stays kept")
}

// A branch checked out in a worktree clean keeps, one dockhand didn't make
// as an adopted branch's, is kept: deleting it would leave that worktree on
// a branch that's gone, which git branch -d refuses to do. So is one
// checked out in your checkout. (The hugo exercise's cleanup after
// beekeeper-studio and ov, where clean left an adopted branch's worktree
// with nothing to stand on.)
func TestCleanKeepsABranchCheckedOutWhereItStays(t *testing.T) {
	_, e, fake, branch := mergedBranch(t)
	branch.Managed = false
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.UpdateBranch(branch) }))
	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"branch dockhand/jq-update":             "it is checked out in " + branch.Worktree + "; switch away there first",
		"ada/macports-ports:dockhand/jq-update": "",
	}, whats(plans), "the worktree isn't dockhand's to remove")
	_, err = e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.Equal(t, "dockhand/jq-update", strings.TrimSpace(run(t, branch.Worktree, "branch", "--show-current")))
	require.NotEmpty(t, run(t, e.Repo.Root, "for-each-ref", "refs/heads/dockhand/jq-update"), "the branch stays")
	require.Empty(t, fake.head("dockhand/jq-update"), "the fork's branch still went")

	// Checked out in your checkout instead, it's kept too.
	run(t, branch.Worktree, "switch", "-q", "--detach")
	run(t, e.Repo.Root, "switch", "-q", "dockhand/jq-update")
	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, "it is checked out in your checkout; switch away first", whats(plans)["branch dockhand/jq-update"])

	// Planned while nowhere else, then checked out before clean ran: the
	// branch is looked for again before it goes.
	run(t, e.Repo.Root, "switch", "-q", "--detach")
	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Empty(t, whats(plans)["branch dockhand/jq-update"])
	run(t, branch.Worktree, "switch", "-q", "dockhand/jq-update")
	done, err := e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.Equal(t, "it is checked out in "+branch.Worktree+"; switch away there first", done[0].Steps[0].Kept)
	require.NotEmpty(t, run(t, e.Repo.Root, "for-each-ref", "refs/heads/dockhand/jq-update"))
}
