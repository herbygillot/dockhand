package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A branch whose files' every change master has, as dockhand last fetched
// it, landed by another route: duckdb-cxx14's C++14 fix was committed to
// master while status still asked to commit it for review (cleaning up
// duckdb-cxx14, finding 1). Status reads the master kept, never fetching;
// a branch master hasn't caught up with, or one that changes nothing,
// isn't on master.
func TestABranchWhoseChangesLandedIsOnMaster(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "duckdb-cxx14"})
	require.NoError(t, err)
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Empty(t, status.OnMaster, "it changes nothing")

	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\nconfigure.cxx_standard 2014\n"})
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Empty(t, status.OnMaster, "master doesn't have it")

	write(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\nconfigure.cxx_standard 2014\n", "devel/other/Portfile": "name other\n"})
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "jq: fix build on macOS 12 and older")
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Empty(t, status.OnMaster, "status fetches nothing")
	master, err := e.fetchMaster(t.Context())
	require.NoError(t, err)
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, master, status.OnMaster)

	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\nconfigure.cxx_standard 2017\n"})
	status, err = e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Empty(t, status.OnMaster, "master has another change")
}
