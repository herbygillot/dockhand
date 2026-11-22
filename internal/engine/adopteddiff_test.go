package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// An adopted pull request's diff is of its Portfiles and PortGroups alone:
// one that changes neither has none, as ReviewDiff has none, where an
// empty selection diffed the whole tree, a README offered as code (#293).
func TestAnAdoptedDiffOfNoPortfileIsEmpty(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "theirs"})
	require.NoError(t, err)
	diff := func(files map[string]string) []byte {
		t.Helper()
		testsupport.WriteFiles(t, branch.Worktree, files)
		testsupport.Git(t, branch.Worktree, "add", "--sparse", "-A")
		testsupport.Git(t, branch.Worktree, "commit", "-qm", "their change")
		adopted := branch
		adopted.PullRequest = &model.PullRequest{Repository: "macports/macports-ports", Number: 7, Adopted: true, AuthorHead: model.ObjectID(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD"))}
		out, err := e.AdoptedDiff(t.Context(), adopted)
		require.NoError(t, err)
		return out
	}
	require.Empty(t, diff(map[string]string{"README.md": "their notes\n"}), "a README is no Portfile")
	require.Contains(t, string(diff(map[string]string{"textproc/jq/Portfile": "name jq\nversion 9\n"})), "textproc/jq/Portfile")
}
