package lifecycle

// Field case (macports-ports-46): a failed run's kept debug VM pinned
// an admission slot forever after the branch was fixed and re-verified
// — CancelStale only looked at running runs, and only at ancestors,
// while the commonest way past a failure is an amend.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

func keptFailureNote(t *testing.T, repo *git.Repo, sha string) {
	t.Helper()
	ctx := context.Background()
	n, err := ledger.Open(repo).LoadOrStart(ctx, sha, "jq")
	require.NoError(t, err)
	n.Runs["Testos"] = record.Run{State: "failed", Handle: "fake-1",
		Job: verify.Job{Provider: "fake", ID: "fake-1"}, Detail: "Failed to build jq: boom"}
	require.NoError(t, ledger.Open(repo).Write(ctx, n))
}

func TestCancelStaleReleasesKeptEnvironmentOfASupersededFailure(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	keptFailureNote(t, repo, sha)

	// The fix lands as a child commit; the old tip is an ancestor.
	fixed := gittest.Commit(t, repo, "dockhand/jq-fix", "dockhand/jq-1.8", "sysutils/jq/Portfile",
		"version 1.8\nrevision 0\n", "jq: drop obsolete patch")
	gittest.MoveBranch(t, repo, "dockhand/jq-1.8", fixed)

	fake := &verifytest.Fake{}
	require.NoError(t, CancelStale(context.Background(), testState(t, repo, fake), repo, "dockhand/jq-1.8", fixed))

	assert.Equal(t, []string{"fake-1"}, fake.Released, "the kept environment is a slot spent on dead code")
	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	r := n.Runs["Testos"]
	assert.Equal(t, record.Superseded, r.State)
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
	amended := gittest.Commit(t, repo, "dockhand/jq-amended", primary, "sysutils/jq/Portfile",
		"version 1.8\nrevision 0\n", "jq: update to 1.8")
	gittest.MoveBranch(t, repo, "dockhand/jq-1.8", amended)
	require.False(t, repo.IsAncestor(context.Background(), sha, "dockhand/jq-1.8"),
		"the fixture must model an amend, not a fixup")

	fake := &verifytest.Fake{}
	require.NoError(t, CancelStale(context.Background(), testState(t, repo, fake), repo, "dockhand/jq-1.8", amended))

	assert.Equal(t, []string{"fake-1"}, fake.Released)
	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	assert.Equal(t, record.Superseded, n.Runs["Testos"].State)
}

// Another branch's kept failure is not this branch's to release.
func TestCancelStaleLeavesOtherBranchesEnvironmentsAlone(t *testing.T) {
	repo, sha := lifecycleRepo(t)
	keptFailureNote(t, repo, sha)

	primary, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	other := gittest.Commit(t, repo, "dockhand/other-1.0", primary, "sysutils/jq/Portfile",
		"version 9.9\n", "other: unrelated")

	fake := &verifytest.Fake{}
	require.NoError(t, CancelStale(context.Background(), testState(t, repo, fake), repo, "dockhand/other-1.0", other))

	assert.Empty(t, fake.Released, "jq's kept environment belongs to jq's branch")
	n, err := ledger.Open(repo).Read(context.Background(), sha)
	require.NoError(t, err)
	assert.Equal(t, record.Failed, n.Runs["Testos"].State)
}
