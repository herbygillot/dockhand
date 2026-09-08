package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bareUpstream gives repo a real remote holding its history, so a fetch
// has somewhere to fetch from. Real git throughout: what is under test
// is what git does with a refspec.
func bareUpstream(t *testing.T, repo *Repo, remote string) (bare, branch string) {
	t.Helper()
	branch, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	bare = filepath.Join(t.TempDir(), "upstream.git")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run(filepath.Dir(bare), "init", "--bare", "--quiet", "--initial-branch="+branch, bare)
	run(repo.Root, "remote", "add", remote, bare)
	run(repo.Root, "push", "--quiet", remote, "HEAD:refs/heads/"+branch)
	return bare, branch
}

// A FETCH UPDATES ONE REMOTE-TRACKING REF AND NOTHING ELSE. That is the
// whole contract a mint leans on: dockhand bases a change on upstream's
// newest tip, and it may do so BY DEFAULT precisely because it moves no
// local branch, reads no working tree and touches no index. Fetching a
// person's checkout out from under them would be this tool reaching into
// a tree it promises not to touch.
func TestFetchBranchMovesOnlyTheTrackingRef(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	bare, branch := bareUpstream(t, repo, "origin")

	before, err := repo.RevParse(ctx, "HEAD")
	require.NoError(t, err)

	// Upstream moves on, the way the project does under a checkout.
	work := t.TempDir()
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run(work, "clone", "--quiet", bare, filepath.Join(work, "c"))
	clone := filepath.Join(work, "c")
	require.NoError(t, os.WriteFile(filepath.Join(clone, "NEW"), []byte("x\n"), 0o644))
	run(clone, "add", ".")
	run(clone, "commit", "--quiet", "-m", "upstream moved")
	run(clone, "push", "--quiet", "origin", "HEAD:refs/heads/"+branch)

	require.NoError(t, repo.FetchBranch(ctx, "origin", branch))

	tracking, err := repo.RevParse(ctx, "origin/"+branch)
	require.NoError(t, err)
	assert.NotEqual(t, before, tracking, "the tracking ref caught up with the remote")

	local, err := repo.RevParse(ctx, "HEAD")
	require.NoError(t, err)
	assert.Equal(t, before, local, "no local ref moved; the person's checkout is where they left it")

	// Behind reads the same fact as a number, which is the sentence a
	// person is shown before their change is cut from the newer commit.
	n, err := repo.Behind(ctx, "HEAD", "origin/"+branch)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

// AND A REMOTE THAT IS NOT THERE IS AN ERROR AND NOT A ZERO. A caller
// that read a silent failure as "already current" would base a change on
// a stale commit and say nothing (rule 7); the one caller there is turns
// this into a sentence and carries on with the local branch.
func TestFetchBranchReportsAFailureRatherThanSucceedingQuietly(t *testing.T) {
	repo := newRepo(t)
	assert.Error(t, repo.FetchBranch(context.Background(), "nowhere", "master"))
}

// Behind IS A COUNT, AND ITS ZERO IS AN ANSWER. A ref that will not
// resolve is a failure, never "nothing is ahead of you".
func TestBehindTellsCurrentFromUnreadable(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	n, err := repo.Behind(ctx, "HEAD", "HEAD")
	require.NoError(t, err)
	assert.Zero(t, n)

	_, err = repo.Behind(ctx, "HEAD", "refs/heads/no-such-branch")
	assert.Error(t, err)
}

// THE UPSTREAM REMOTE IS THE PRIMARY BRANCH'S, ELSE ORIGIN. A checkout
// like the one this tool is used on carries six remotes — upstream, the
// maintainer's fork, and four other people's — so which of them is
// upstream is a question with a wrong answer, and it is answered in one
// place that gh and the mint both ask.
func TestPrimaryRemoteFollowsTheBranchThenFallsBackToOrigin(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	got, err := repo.PrimaryRemote(ctx)
	require.NoError(t, err)
	assert.Equal(t, "origin", got, "a clone that set no upstream is git's own default")

	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	_, _ = bareUpstream(t, repo, "somewhere-else")
	require.NoError(t, repo.SetConfig(ctx, "branch."+primary+".remote", "somewhere-else"))

	got, err = repo.PrimaryRemote(ctx)
	require.NoError(t, err)
	assert.Equal(t, "somewhere-else", got, "the branch's own tracking config decides")
}
