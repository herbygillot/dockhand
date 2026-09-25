package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEditRevbumpRetryRebaseAndArchive(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")

	_, _, err := dockhand(t, "revbump", "jq")
	require.ErrorContains(t, err, "revbump needs the reason as --subject")
	out, _, err := dockhand(t, "revbump", "jq", "--subject", "rebuild for oniguruma 6.9.10")
	require.NoError(t, err, "outside any branch, revbump starts one")
	require.Regexp(t, `^Started dockhand/jq-[a-z0-9]{4} from master `, out)
	require.Contains(t, out, "· 1 port\n  jq  revision 0 → 1\nRecorded the subject for tidy: \"<port>: rebuild for oniguruma 6.9.10\"\n")

	_, _, err = dockhand(t, "start", "notes")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "src", "macports-branches", "notes")
	t.Setenv("MACPORTS_TREE", dir)
	out, _, err = dockhand(t, "edit", "jq")
	require.NoError(t, err, "without a terminal, edit prints the Portfile")
	require.Equal(t, filepath.Join(dir, "textproc/jq/Portfile")+"\n", out)
	require.FileExists(t, filepath.Join(dir, "textproc/jq/Portfile"), "the sparse worktree grew to hold it")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n# a note\n"), 0o644))
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)
	out, errs, err := dockhand(t, "retry", "check-1")
	require.NoError(t, err)
	require.Contains(t, out, "check-2 repeats check-1: snapshot 1\n")
	require.Contains(t, errs, "check-2 runs here")
	require.Contains(t, out, "Passed for snapshot 1.")

	_, _, err = dockhand(t, "rebase")
	require.ErrorContains(t, err, "has uncommitted edits")
	_, _, err = dockhand(t, "tidy", "--squash", "--message", "jq: note the build")
	require.NoError(t, err)
	out, _, err = dockhand(t, "rebase")
	require.NoError(t, err)
	require.Contains(t, out, "notes already starts from master ")
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "README"), []byte("new\n"), 0o644))
	gitRun(t, w.upstream, "add", "README")
	gitRun(t, w.upstream, "commit", "-q", "-m", "README")
	out, _, err = dockhand(t, "rebase")
	require.NoError(t, err)
	require.Regexp(t, `Rebased notes \(1 commit\) from master [0-9a-f]{7} onto [0-9a-f]{7}\.\nCheckpoint rebase-2 keeps the old history \(dockhand restore rebase-2\)\.\n`, out)

	out, _, err = dockhand(t, "archive")
	require.NoError(t, err)
	require.Contains(t, out, "Archived notes;")
	t.Setenv("MACPORTS_TREE", w.clone)
	out, _, err = dockhand(t, "status")
	require.NoError(t, err)
	require.NotContains(t, out, "notes ")
	out, _, err = dockhand(t, "archive", "--undo", "notes")
	require.NoError(t, err)
	require.Equal(t, "notes is back among your open branches.\n", out)
}
