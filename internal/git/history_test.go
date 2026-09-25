package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHistoryAndComposeTree(t *testing.T) {
	repo, base := portsCheckout(t)
	require.NoError(t, os.WriteFile(filepath.Join(repo.Root, "textproc/jq/Portfile"), []byte("name jq\nversion 2\n"), 0o644))
	gitIn(t, repo.Root, "commit", "-q", "-am", "Update jq")
	require.NoError(t, os.Remove(filepath.Join(repo.Root, "devel/libharbor/Portfile")))
	require.NoError(t, os.WriteFile(filepath.Join(repo.Root, "textproc/jq/Portfile"), []byte("name jq\nversion 3\n"), 0o644))
	gitIn(t, repo.Root, "commit", "-q", "-am", "oops\n\nCloses: https://trac.macports.org/ticket/71234")
	head, err := repo.Resolve(t.Context(), "HEAD")
	require.NoError(t, err)

	history, err := repo.History(t.Context(), base, head)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "Update jq", history[0].Subject())
	require.Equal(t, []string{"textproc/jq/Portfile"}, history[0].Paths)
	require.Equal(t, "Test", history[0].Author.Name)
	require.False(t, history[0].Merge())
	require.ElementsMatch(t, []string{"devel/libharbor/Portfile", "textproc/jq/Portfile"}, history[1].Paths)
	require.Contains(t, history[1].Message, "Closes: https://trac.macports.org/ticket/71234\n")

	// Take only jq's change onto the base.
	trees, err := repo.CommitTrees(t.Context(), []string{base, head})
	require.NoError(t, err)
	partial, err := repo.ComposeTree(t.Context(), trees[base], trees[head], []string{"textproc/jq/Portfile"})
	require.NoError(t, err)
	changed, err := repo.ChangedPaths(t.Context(), trees[base], partial)
	require.NoError(t, err)
	require.Equal(t, []string{"textproc/jq/Portfile"}, changed)
	whole, err := repo.ComposeTree(t.Context(), partial, trees[head], []string{"devel/libharbor/Portfile"})
	require.NoError(t, err)
	require.Equal(t, trees[head], whole, "the removal is carried too")
}
