package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestEditRevbumpRetryRebaseAndArchive(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")

	_, _, err := cmdtest.Dockhand(t, "revbump", "jq")
	require.ErrorContains(t, err, "revbump needs the reason as --subject")
	out, _, err := cmdtest.Dockhand(t, "revbump", "jq", "--subject", "rebuild for oniguruma 6.9.10")
	require.NoError(t, err, "outside any branch, revbump starts one")
	require.Regexp(t, `^Started dockhand/jq-rebuild from master `, out)
	require.Contains(t, out, "· 1 port\n  jq  revision 0 → 1\nRecorded the subject for tidy: \"<port>: rebuild for oniguruma 6.9.10\"\n")

	_, _, err = cmdtest.Dockhand(t, "start", "notes")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "notes")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	out, _, err = cmdtest.Dockhand(t, "edit", "jq")
	require.NoError(t, err, "without a terminal, edit prints the Portfile")
	require.Equal(t, filepath.Join(dir, "textproc/jq/Portfile")+"\n", out)
	require.FileExists(t, filepath.Join(dir, "textproc/jq/Portfile"), "the sparse worktree grew to hold it")
	cmdtest.Setenv(t, "EDITOR", "false")
	out, _, err = cmdtest.Dockhand(t, "edit", "jq", "--no-open")
	require.NoError(t, err, "--no-open runs no editor, failing or not")
	require.Equal(t, filepath.Join(dir, "textproc/jq/Portfile")+"\n", out)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n# a note\n"), 0o644))
	_, _, err = cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	out, errs, err := cmdtest.Dockhand(t, "retry", "check-1")
	require.NoError(t, err)
	require.Contains(t, out, "check-2 repeats check-1: snapshot 1\n")
	require.Contains(t, errs, "check-2 runs here")
	require.Contains(t, out, "Passed for snapshot 1.")

	_, _, err = cmdtest.Dockhand(t, "rebase")
	require.ErrorContains(t, err, "has uncommitted edits")
	_, _, err = cmdtest.Dockhand(t, "tidy", "--squash", "--message", "jq: note the build")
	require.NoError(t, err)
	out, _, err = cmdtest.Dockhand(t, "rebase")
	require.NoError(t, err)
	require.Contains(t, out, "notes already starts from master ")
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "README"), []byte("new\n"), 0o644))
	testsupport.Git(t, w.Upstream, "add", "README")
	testsupport.Git(t, w.Upstream, "commit", "-q", "-m", "README")
	out, _, err = cmdtest.Dockhand(t, "rebase")
	require.NoError(t, err)
	require.Regexp(t, `Rebased notes \(1 commit\) from master [0-9a-f]{7} onto [0-9a-f]{7}\.\nCheckpoint rebase-2 keeps the old history \(dockhand undo rebase-2\)\.\n`, out)
	require.Contains(t, out, "Next: dockhand check (the files it builds on have changed)\n")
	_, _, err = cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	oldMaster := testsupport.Git(t, w.Upstream, "rev-parse", "--short=7", "HEAD~1")

	// Restoring a rebase puts back its history, files, and master.
	out, _, err = cmdtest.Dockhand(t, "undo", "rebase-2")
	require.NoError(t, err)
	require.Regexp(t, `^Restored dockhand/notes to its history and files before rebase-2 \([0-9a-f]{7}\), on master `+oldMaster+` again\.\n$`, out)
	require.NoFileExists(t, filepath.Join(dir, "README"), "master's newer file isn't left as the branch's edit")

	// Rebasing again makes files a check has seen, which it names (the gh
	// rebase's finding 3).
	out, _, err = cmdtest.Dockhand(t, "rebase")
	require.NoError(t, err)
	require.Contains(t, out, "Checkpoint rebase-3 keeps the old history")
	require.Contains(t, out, "check-3 checked these files already: passed for this commit.\nNext: fork macports/macports-ports on GitHub and add it as a Git remote, then dockhand submit --branch notes\n", "no remote pushes to a fork in this world")
	require.NotContains(t, out, "Next: dockhand check")

	out, _, err = cmdtest.Dockhand(t, "archive")
	require.NoError(t, err)
	require.Contains(t, out, "Archived notes, and removed its worktree;")
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	out, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.NotContains(t, out, "notes ")
	out, _, err = cmdtest.Dockhand(t, "archive", "--undo", "notes")
	require.NoError(t, err)
	require.Equal(t, "notes is back among your open branches.\n", out)
}

// start --port brings a port's directory in from the start, so a script
// needn't run edit for the files; with no name, the branch is named for
// the port, as an edit's is.
func TestStartBringsItsPortsIn(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	out, _, err := cmdtest.Dockhand(t, "start", "notes", "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "Created dockhand/notes from master ")
	require.FileExists(t, filepath.Join(w.Home, "Source", "macports-branches", "notes", "textproc/jq/Portfile"))
	out, _, err = cmdtest.Dockhand(t, "start", "--port", "jq")
	require.NoError(t, err)
	require.Regexp(t, `^Created dockhand/jq-[a-z0-9]{4} from master `, out)
	_, _, err = cmdtest.Dockhand(t, "start")
	require.ErrorContains(t, err, "start needs a branch name or --port <port> to generate one.")
}

// undo takes back the branch's latest tidy, its checkpoint found for it;
// open opens a branch's pull request, and says one it hasn't.
func TestUndoAndOpen(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	out, _, err := cmdtest.Dockhand(t, "undo")
	require.NoError(t, err)
	require.Regexp(t, `^Restored dockhand/jq-update to its history before tidy-\d+ \([0-9a-f]+\)\. The files are unchanged\.\n$`, out)
	_, _, err = cmdtest.Dockhand(t, "undo")
	require.ErrorContains(t, err, "jq-update has no tidy or rebase to undo")

	var opened []string
	cmdtest.Hosted(t).OpenBrowser = func(_ context.Context, address string) error {
		opened = append(opened, address)
		return nil
	}
	_, _, err = cmdtest.Dockhand(t, "open")
	require.ErrorContains(t, err, "jq-update has no pull request yet; dockhand submit -b jq-update opens one")
	require.Empty(t, opened)
}

// A branch started for a port is that port's before it changes it: status
// --port finds it, status says what it was started for, and an edit of the
// port from the checkout works in it rather than starting a second beside
// it (the command-line UX second look's §1).
func TestABranchStartedForAPortIsThatPorts(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	_, _, err := cmdtest.Dockhand(t, "start", "notes", "--port", "jq")
	require.NoError(t, err)
	out, _, err := cmdtest.Dockhand(t, "status", "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "notes in ~/Source/macports-branches/notes\n", "found, and the one shown in detail")
	require.Contains(t, out, "  Ports    none changed yet; started for jq\n")
	out, _, err = cmdtest.Dockhand(t, "status", "notes")
	require.NoError(t, err)
	require.Contains(t, out, "  Ports    none changed yet; started for jq\n")
	result, err := cmdtest.JSONOf(t, "status", "notes")
	require.NoError(t, err)
	require.Equal(t, []any{"jq"}, cmdtest.Dig(t, result.Result, "branches", 0, "started_for"))

	_, errs, err := cmdtest.Dockhand(t, "edit", "jq")
	require.NoError(t, err)
	require.Contains(t, errs, "Working in notes, the one open branch changing jq; --new starts another.\n")
}

// Where several open branches change a port, one question asks which, or
// for a new one: "start another?" and "which one?" were two (the split's
// 7d row 7). A script is told them, by exact name.
func TestSeveralBranchesForAPortAreOneQuestion(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	for _, name := range []string{"jq-one", "jq-two"} {
		_, _, err := cmdtest.Dockhand(t, "start", name, "--port", "jq")
		require.NoError(t, err)
	}
	run := func(answer string) (string, string, error) {
		var out, errs bytes.Buffer
		err := cmdtest.RunFor(t, t.Context(), []string{"edit", "jq", "--no-open"}, term.Streams{In: strings.NewReader(answer + "\n"), Out: &out, Err: &errs}.AsTerminal())
		return out.String(), errs.String(), err
	}
	_, errs, err := run("2")
	require.NoError(t, err)
	require.Contains(t, errs, "jq is changed in 2 open branches:\n  1. jq-one\n  2. jq-two\n? which one, or n for a new branch? [1-2/n] ")
	out, _, err := run("n")
	require.NoError(t, err)
	require.Regexp(t, `(?m)^Started dockhand/jq-[a-z0-9]{4} from master`, out)
	_, _, err = cmdtest.Dockhand(t, "edit", "jq", "--no-open")
	require.ErrorIs(t, err, engine.ErrAmbiguous, "a script is told them")
}
