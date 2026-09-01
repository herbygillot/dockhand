package lifecycle

// Field case (macports-ports-46): a failed run's kept debug VM pinned
// an admission slot forever after the branch was fixed and re-verified
// — CancelStale only looked at running runs, and only at ancestors,
// while the commonest way past a failure is an amend.

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

func keptFailureNote(t *testing.T, repo *git.Repo, sha string) {
	t.Helper()
	ctx := context.Background()
	n, err := LoadOrStartNote(ctx, repo, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = Run{State: "failed", Handle: "fake-1",
		Job: verify.Job{Provider: "fake", ID: "fake-1"}, Detail: "Failed to build jq: boom"}
	require.NoError(t, WriteNote(ctx, repo, n))
}

// moveBranch repoints the branch at sha the way an amend or a fixup
// does, reflog entry included.
func moveBranch(t *testing.T, repo *git.Repo, branch, sha string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repo.Root, "update-ref",
		"refs/heads/"+branch, sha)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
}

func TestCancelStaleReleasesKeptEnvironmentOfASupersededFailure(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	keptFailureNote(t, repo, sha)

	// The fix lands as a child commit; the old tip is an ancestor.
	fixed, err := repo.Mint(context.Background(), git.MintRequest{
		Branch: "dockhand/jq-fix", Base: "dockhand/jq-1.8", Path: "sysutils/jq/Portfile",
		Content: []byte("version 1.8\nrevision 0\n"), Message: "jq: drop obsolete patch",
	})
	require.NoError(t, err)
	moveBranch(t, repo, "dockhand/jq-1.8", fixed)

	fake := &verifytest.Fake{}
	require.NoError(t, CancelStale(context.Background(), testState(t, fake), repo, "dockhand/jq-1.8", fixed))

	assert.Equal(t, []string{"fake-1"}, fake.Released, "the kept environment is a slot spent on dead code")
	n, err := ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	r := n.Runs["Testos"]
	assert.Equal(t, "superseded", r.State)
	assert.Empty(t, r.Handle)
	assert.Contains(t, r.Detail, "kept environment released")
	assert.Contains(t, r.Detail, "failed here")
}

// The amend shape: the failed sha is no longer reachable from the
// branch at all, and only the reflog remembers the branch held it.
func TestCancelStaleReachesAmendedAwayFailures(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	keptFailureNote(t, repo, sha)

	primary, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	amended, err := repo.Mint(context.Background(), git.MintRequest{
		Branch: "dockhand/jq-amended", Base: primary, Path: "sysutils/jq/Portfile",
		Content: []byte("version 1.8\nrevision 0\n"), Message: "jq: update to 1.8",
	})
	require.NoError(t, err)
	moveBranch(t, repo, "dockhand/jq-1.8", amended)
	require.False(t, repo.IsAncestor(context.Background(), sha, "dockhand/jq-1.8"),
		"the fixture must model an amend, not a fixup")

	fake := &verifytest.Fake{}
	require.NoError(t, CancelStale(context.Background(), testState(t, fake), repo, "dockhand/jq-1.8", amended))

	assert.Equal(t, []string{"fake-1"}, fake.Released)
	n, err := ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "superseded", n.Runs["Testos"].State)
}

// Another branch's kept failure is not this branch's to release.
func TestCancelStaleLeavesOtherBranchesEnvironmentsAlone(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	keptFailureNote(t, repo, sha)

	primary, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	other, err := repo.Mint(context.Background(), git.MintRequest{
		Branch: "dockhand/other-1.0", Base: primary, Path: "sysutils/jq/Portfile",
		Content: []byte("version 9.9\n"), Message: "other: unrelated",
	})
	require.NoError(t, err)

	fake := &verifytest.Fake{}
	require.NoError(t, CancelStale(context.Background(), testState(t, fake), repo, "dockhand/other-1.0", other))

	assert.Empty(t, fake.Released, "jq's kept environment belongs to jq's branch")
	n, err := ReadNote(context.Background(), repo, sha)
	require.NoError(t, err)
	assert.Equal(t, "failed", n.Runs["Testos"].State)
}
