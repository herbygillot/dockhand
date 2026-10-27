package engine

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A merged branch with a commit past its merge names start --from next,
// which starts a branch from master with that commit picked onto it; one
// with nothing past its merge names none, and start --from refuses it
// (Then's item 7: wasmedge's follow-up was made by hand).
func TestAFollowUpStartsFromTheCommitsPastAMerge(t *testing.T) {
	t.Parallel()
	f, e, _, branch := mergedBranch(t)
	_, err := e.StartFrom(t.Context(), "jq-more", branch)
	require.ErrorContains(t, err, "has no commits past what #")

	// Master has the merge, as GitHub made it.
	portfile := read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile"))
	write(t, f.upstream, map[string]string{"textproc/jq/Portfile": portfile})
	testsupport.Git(t, f.upstream, "commit", "-q", "-am", "jq: update to 1.8.1")
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": portfile + "revision 1\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "jq: a fix after the merge")
	status, err := e.BranchStatus(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, 1, status.PastMerge)
	rows := branchstate.Attend(status.Facts())
	require.Len(t, rows, 1)
	require.Equal(t, branchstate.WorkPastMerge, rows[0].Reason)
	require.Equal(t, []string{"--from", "jq-update"}, rows[0].Next.Args)

	done, err := e.StartFrom(t.Context(), "jq-more", branch)
	require.NoError(t, err)
	require.Len(t, done.Picked, 1)
	require.Equal(t, "jq: a fix after the merge\n", done.Picked[0].Message)
	require.Equal(t, portfile+"revision 1\n", read(t, filepath.Join(done.Branch.Worktree, "textproc/jq/Portfile")), "the port's directory is checked out, with the commit")
	require.Equal(t, string(done.Branch.Base), testsupport.Git(t, done.Branch.Worktree, "rev-parse", "HEAD^"), "on master")
}
