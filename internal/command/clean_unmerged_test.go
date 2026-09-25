package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanTakesAnArchivedBranchsWorktreeAndPathBringsItBack(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "src", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	head := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))
	t.Setenv("MACPORTS_TREE", w.clone)
	_, _, err = dockhand(t, "archive", "jq-update")
	require.NoError(t, err)

	// Plain clean is merged branches only.
	out, _, err := dockhand(t, "clean")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out)

	// A worktree with edits of its own stays.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine\n"), 0o644))
	out, _, err = dockhand(t, "clean", "--archived", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update (archived)\n  keep     worktree ~/src/macports-branches/jq-update: it has untracked files: notes.txt\n")
	require.DirExists(t, dir)
	require.NoError(t, os.Remove(filepath.Join(dir, "notes.txt")))

	out, _, err = dockhand(t, "clean", "--archived")
	require.NoError(t, err)
	require.Equal(t, "jq-update (archived)\n  remove   worktree ~/src/macports-branches/jq-update\n"+
		"  keep     branch dockhand/jq-update: dockhand path jq-update checks it out again\n"+
		"Nothing was removed; --yes removes these.\n", out)
	_, _, err = dockhand(t, "clean", "--archived", "--yes")
	require.NoError(t, err)
	require.NoDirExists(t, dir)
	require.Equal(t, head, strings.TrimSpace(gitRun(t, w.clone, "rev-parse", "dockhand/jq-update")), "the branch and its work stay")

	out, _, err = dockhand(t, "path", "jq-update")
	require.NoError(t, err)
	require.Equal(t, dir+"\n", strings.Replace(out, "/private", "", 1))
	data, err := os.ReadFile(filepath.Join(dir, "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "name jq\nversion 1.8.1\n", string(data), "checked out again, sparse over the ports it changes")
}
