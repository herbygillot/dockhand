package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The guards that keep clean from removing what a person still has: each
// a variation on a merged branch (the test plan's step 2, items 1 to 6).

// A fork's branch that moved after the merge, before clean or while it
// ran, is kept, and holds what it moved to.
func TestCleanKeepsAForkBranchThatMoved(t *testing.T) {
	t.Parallel()
	_, e, fake, branch := mergedBranch(t)
	other := testsupport.Git(t, fake.Fork, "rev-parse", "master")
	testsupport.Git(t, fake.Fork, "update-ref", "refs/heads/dockhand/jq-update", other)
	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, "it has moved since the merge", whats(plans)["ada/macports-ports:dockhand/jq-update"])
	_, err = e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.Equal(t, model.ObjectID(other), fake.ForkHead("dockhand/jq-update"))
	require.NoDirExists(t, branch.Worktree, "the rest goes")

	_, e, fake, _ = mergedBranch(t)
	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Empty(t, whats(plans)["ada/macports-ports:dockhand/jq-update"])
	other = testsupport.Git(t, fake.Fork, "rev-parse", "master")
	testsupport.Git(t, fake.Fork, "update-ref", "refs/heads/dockhand/jq-update", other)
	done, err := e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.Equal(t, "it moved while clean ran", whats(done)["ada/macports-ports:dockhand/jq-update"])
	require.Equal(t, model.ObjectID(other), fake.ForkHead("dockhand/jq-update"))
}

// A fork's branch is kept where no remote reaches the fork, or the one
// that does can't be read.
func TestCleanKeepsAForkBranchItCantReach(t *testing.T) {
	t.Parallel()
	f, e, _, _ := mergedBranch(t)
	testsupport.Git(t, f.clone, "remote", "remove", "fork")
	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, "no Git remote pushes to ada/macports-ports", whats(plans)["ada/macports-ports:dockhand/jq-update"])

	_, e, fake, _ := mergedBranch(t)
	require.NoError(t, os.Rename(fake.Fork, fake.Fork+".gone"))
	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Contains(t, whats(plans)["ada/macports-ports:dockhand/jq-update"], "it could not be read: ")
	require.NoError(t, os.Rename(fake.Fork+".gone", fake.Fork))
	_, err = e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.NotEmpty(t, fake.ForkHead("dockhand/jq-update"), "kept")
}

// The branches the github provider left in the fork for a branch's checks
// go only while each holds the commit it checked, and are kept where the
// fork can't be reached or read.
func TestCleanKeepsCheckBranchesThatMoved(t *testing.T) {
	t.Parallel()
	checked := func(t *testing.T) (*Engine, string, string, string) {
		f, e, fake, branch := mergedBranch(t)
		e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}}
		e.Providers = map[string]buildenv.Provider{"github": &scriptedProvider{}}
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
		require.NoError(t, err)
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{{Provider: "github", Platform: tahoeArm.Platform}}})
		require.NoError(t, err)
		_, err = e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
		require.NoError(t, err)
		commit := string(capture.Revision.Source.Commit)
		name := buildenv.CheckBranchPrefix + commit[:12]
		testsupport.Git(t, fake.Fork, "update-ref", "refs/heads/"+name, commit)
		return e, fake.Fork, f.clone, name
	}

	e, fork, _, name := checked(t)
	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Contains(t, whats(plans), "ada/macports-ports:"+name)
	require.Empty(t, whats(plans)["ada/macports-ports:"+name], "it still holds the commit checked")
	other := testsupport.Git(t, fork, "rev-parse", "master")
	testsupport.Git(t, fork, "update-ref", "refs/heads/"+name, other)
	done, err := e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.Equal(t, "it moved while clean ran", whats(done)["ada/macports-ports:"+name])

	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, "it has moved since the check", whats(plans)["ada/macports-ports:"+name])

	e, fork, _, name = checked(t)
	require.NoError(t, os.Rename(fork, fork+".gone"))
	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Contains(t, whats(plans)["ada/macports-ports:"+name], "it could not be read: ")
	require.NoError(t, os.Rename(fork+".gone", fork))

	e, _, clone, name := checked(t)
	testsupport.Git(t, clone, "remote", "remove", "fork")
	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	require.Equal(t, "no Git remote pushes to ada/macports-ports", whats(plans)["ada/macports-ports:"+name])
}

// A merged branch whose merged commit isn't known, neither pushed nor
// observed, keeps its worktree and branch, and nothing of the fork is
// planned, since nothing says what it should hold.
func TestCleanKeepsABranchWhoseMergedCommitIsntKnown(t *testing.T) {
	t.Parallel()
	_, e, _, branch := mergedBranch(t)
	pr := *branch.PullRequest
	pr.Pushed, pr.Observed = "", nil
	branch.PullRequest = &pr
	plan, err := e.planCleanBranch(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"worktree " + branch.Worktree: "the merged commit is not known",
		"branch dockhand/jq-update":   "the merged commit is not known",
	}, whats([]CleanBranch{plan}))
}

// What clean finds while it runs keeps what it would have removed: a
// worktree that took an edit keeps itself and its branch, and a branch
// that took a commit is kept as moved.
func TestCleanChecksAgainAsItRemoves(t *testing.T) {
	t.Parallel()
	_, e, fake, branch := mergedBranch(t)
	plans, err := e.PlanClean(t.Context())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(branch.Worktree, "notes.txt"), []byte("mine\n"), 0o644))
	done, err := e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"worktree " + branch.Worktree:           "it has untracked files: notes.txt",
		"branch dockhand/jq-update":             "the worktree it is checked out in is kept",
		"ada/macports-ports:dockhand/jq-update": "",
	}, whats(done))
	require.DirExists(t, branch.Worktree)
	require.Empty(t, fake.ForkHead("dockhand/jq-update"), "the fork's branch held the merge, so it went")

	f, e, _, branch := mergedBranch(t)
	plans, err = e.PlanClean(t.Context())
	require.NoError(t, err)
	testsupport.Git(t, branch.Worktree, "commit", "-q", "--allow-empty", "-m", "more")
	moved := testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD")
	done, err = e.ApplyClean(t.Context(), plans)
	require.NoError(t, err)
	require.Equal(t, "it moved while clean ran", whats(done)["branch dockhand/jq-update"])
	require.Equal(t, moved, testsupport.Git(t, f.clone, "rev-parse", "dockhand/jq-update"), "the commit is still there")
}

// A branch from before v3 whose change master has goes, but not the
// fork's branch of the same name where it holds another commit; and one
// that moved while clean ran is kept, the fork's likewise.
func TestLegacyCleanupKeepsWhatHoldsAnotherCommit(t *testing.T) {
	t.Parallel()
	legacy := func(t *testing.T) (fixture, *Engine, string) {
		f := setup(t)
		e := f.open(t)
		f.withFork(t, e)
		name := "dockhand/bump/libharbor-4f2a"
		testsupport.Git(t, f.clone, "switch", "-q", "-c", name, "master")
		write(t, f.clone, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 2\n"})
		testsupport.Git(t, f.clone, "commit", "-q", "-am", "libharbor: update to 2, mine")
		testsupport.Git(t, f.clone, "push", "-q", "fork", name)
		testsupport.Git(t, f.clone, "switch", "-q", "master")
		return f, e, name
	}

	f, e, name := legacy(t)
	testsupport.Git(t, f.clone, "push", "-q", "-f", "fork", "master:"+name)
	plans, err := e.PlanLegacy(t.Context())
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, LegacyOnMaster, plans[0].Kind)
	require.Equal(t, "ada/macports-ports:"+name+" holds another commit", plans[0].ForkKept)
	done, err := e.RemoveLegacy(t.Context(), plans)
	require.NoError(t, err)
	require.True(t, done[0].Done)
	require.NotEmpty(t, testsupport.Git(t, f.clone, "ls-remote", "fork", "refs/heads/"+name), "the fork's, holding another commit, stays")

	f, e, name = legacy(t)
	plans, err = e.PlanLegacy(t.Context())
	require.NoError(t, err)
	require.Empty(t, plans[0].ForkKept)
	testsupport.Git(t, f.clone, "push", "-q", "-f", "fork", "master:"+name)
	done, err = e.RemoveLegacy(t.Context(), plans)
	require.NoError(t, err)
	require.Equal(t, "ada/macports-ports:"+name+" moved while clean ran", done[0].ForkKept)
	require.True(t, done[0].Done, "the local branch went")

	f, e, name = legacy(t)
	plans, err = e.PlanLegacy(t.Context())
	require.NoError(t, err)
	testsupport.Git(t, f.clone, "branch", "-q", "-f", name, "master")
	done, err = e.RemoveLegacy(t.Context(), plans)
	require.NoError(t, err)
	require.Equal(t, "it moved while clean ran", done[0].Kept)
	require.False(t, done[0].Done)
	require.NotEmpty(t, testsupport.Git(t, f.clone, "branch", "--list", name))
}
