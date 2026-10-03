package command

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
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
	require.Regexp(t, `^Started dockhand/jq-rebuild from master `, out)
	require.Contains(t, out, "· 1 port\n  jq  revision 0 → 1\nRecorded the subject for tidy: \"<port>: rebuild for oniguruma 6.9.10\"\n")

	_, _, err = dockhand(t, "start", "notes")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "notes")
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
	testsupport.Git(t, w.upstream, "add", "README")
	testsupport.Git(t, w.upstream, "commit", "-q", "-m", "README")
	out, _, err = dockhand(t, "rebase")
	require.NoError(t, err)
	require.Regexp(t, `Rebased notes \(1 commit\) from master [0-9a-f]{7} onto [0-9a-f]{7}\.\nCheckpoint rebase-2 keeps the old history \(dockhand undo rebase-2\)\.\n`, out)
	require.Contains(t, out, "Next: dockhand check (the files it builds on have changed)\n")
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)
	oldMaster := testsupport.Git(t, w.upstream, "rev-parse", "--short=7", "HEAD~1")
	newMaster := testsupport.Git(t, w.upstream, "rev-parse", "--short=7", "HEAD")

	// Restoring a rebase puts back its history, files, and master.
	out, _, err = dockhand(t, "restore", "rebase-2")
	require.NoError(t, err)
	require.Regexp(t, `^Restored dockhand/notes to its history and files before rebase-2 \([0-9a-f]{7}\), on master `+oldMaster+` again\.\n$`, out)
	require.NoFileExists(t, filepath.Join(dir, "README"), "master's newer file isn't left as the branch's edit")

	// Rebasing again makes files a check has seen, which it names (the gh
	// rebase's finding 3).
	out, _, err = dockhand(t, "rebase")
	require.NoError(t, err)
	require.Contains(t, out, "Checkpoint rebase-3 keeps the old history")
	require.Contains(t, out, "check-3 checked these files already: passed for this commit.\nNext: fork macports/macports-ports on GitHub and add it as a Git remote, then dockhand submit --branch notes\n", "no remote pushes to a fork in this world")
	require.NotContains(t, out, "Next: dockhand check")

	// One recorded before checkpoints kept the base says what it leaves.
	db, err := sql.Open("sqlite", filepath.Join(w.home, ".dockhand", "dockhand.db"))
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "UPDATE checkpoints SET base_before='', base_after='' WHERE number=3")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	out, _, err = dockhand(t, "restore", "rebase-3")
	require.NoError(t, err)
	require.Contains(t, out, ".\nrebase-3 didn't record the master it moved dockhand/notes from, so dockhand still takes it to start from master "+newMaster+". dockhand rebase puts its commits there.\n")

	out, _, err = dockhand(t, "archive")
	require.NoError(t, err)
	require.Contains(t, out, "Archived notes, and removed its worktree;")
	t.Setenv("MACPORTS_TREE", w.clone)
	out, _, err = dockhand(t, "status")
	require.NoError(t, err)
	require.NotContains(t, out, "notes ")
	out, _, err = dockhand(t, "archive", "--undo", "notes")
	require.NoError(t, err)
	require.Equal(t, "notes is back among your open branches.\n", out)
}

// start --port brings a port's directory in from the start, so a script
// needn't run edit for the files; with no name, the branch is named for
// the port, as an edit's is.
func TestStartBringsItsPortsIn(t *testing.T) {
	w := newWorld(t)
	out, _, err := dockhand(t, "start", "notes", "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "Created dockhand/notes from master ")
	require.FileExists(t, filepath.Join(w.home, "Source", "macports-branches", "notes", "textproc/jq/Portfile"))
	out, _, err = dockhand(t, "start", "--port", "jq")
	require.NoError(t, err)
	require.Regexp(t, `^Created dockhand/jq-[a-z0-9]{4} from master `, out)
	_, _, err = dockhand(t, "start")
	require.ErrorContains(t, err, "start needs a name, or a --port to name the branch for")
}

// undo takes back the branch's latest tidy, its checkpoint found for it;
// open opens a branch's pull request, and says one it hasn't.
func TestUndoAndOpen(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	out, _, err := dockhand(t, "undo")
	require.NoError(t, err)
	require.Regexp(t, `^Restored dockhand/jq-update to its history before tidy-\d+ \([0-9a-f]+\)\. The files are unchanged\.\n$`, out)
	_, _, err = dockhand(t, "undo")
	require.ErrorContains(t, err, "jq-update has no tidy or rebase to undo")

	var opened []string
	real := openBrowser
	t.Cleanup(func() { openBrowser = real })
	openBrowser = func(_ context.Context, address string) error {
		opened = append(opened, address)
		return nil
	}
	_, _, err = dockhand(t, "open")
	require.ErrorContains(t, err, "jq-update has no pull request yet; dockhand submit -b jq-update opens one")
	require.Empty(t, opened)
}
