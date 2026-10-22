package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// archive takes a branch's worktree where it holds nothing the branch's
// commits don't, in one step (the command-line UX review, §5), keeping
// the Git branch, and path checks it out again. One with edits of its own
// stays, saying why, unless --discard; and clean, naming it, takes the
// worktree path checked out again.
func TestArchiveTakesTheWorktreeAndPathBringsItBack(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	setenv(t, "MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	head := strings.TrimSpace(testsupport.Git(t, dir, "rev-parse", "HEAD"))
	setenv(t, "MACPORTS_TREE", w.clone)

	// A worktree with edits of its own stays, and says why.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine\n"), 0o644))
	archived, _, err := dockhand(t, "archive", "jq-update")
	require.NoError(t, err)
	require.Contains(t, archived, "Archived jq-update; its worktree stays, since it has untracked files: notes.txt. dockhand archive --discard jq-update removes it, edits and all.\n")
	require.DirExists(t, dir)
	out, _, err := dockhand(t, "clean")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out, "plain clean is merged branches only")

	// What Git ignores goes with the worktree, and is said, as nothing
	// else lists it (the person's .gitignore item, 2026-10-04).
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.log"), []byte("built\n"), 0o644))
	archived, _, err = dockhand(t, "archive", "--discard", "jq-update")
	require.NoError(t, err)
	require.Contains(t, archived, "Archived jq-update, and removed its worktree; its Git branch and pull request stay. dockhand archive --undo jq-update brings it back.\nIts ignored files went with it: build.log\n")
	require.NoDirExists(t, dir)
	require.Equal(t, head, strings.TrimSpace(testsupport.Git(t, w.clone, "rev-parse", "dockhand/jq-update")), "the branch and its work stay")

	// status --all says it's archived, which its row didn't (the dogfood
	// run with bf711891).
	out, _, err = dockhand(t, "status", "--all")
	require.NoError(t, err)
	require.Regexp(t, `(?m)^jq-update \(archived\)\s+1\s+1 commit\s`, out)

	out, _, err = dockhand(t, "path", "jq-update")
	require.NoError(t, err)
	// macOS's temporary directories are under /private, which one side may
	// name and the other not.
	require.Equal(t, strings.TrimPrefix(dir, "/private")+"\n", strings.TrimPrefix(out, "/private"))
	data, err := os.ReadFile(filepath.Join(dir, "textproc/jq/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "name jq\nversion 1.8.1\n", string(data), "checked out again, sparse over the ports it changes")

	// Its worktree, checked out again, is clean's when named.
	out, _, err = dockhand(t, "clean", "jq-update")
	require.Equal(t, term.ExitFailure, term.ExitCode(err))
	require.Equal(t, "jq-update (archived)\n  remove   worktree ~/Source/macports-branches/jq-update\n"+
		"  keep     branch dockhand/jq-update: dockhand path jq-update checks it out again\n", out)
	out, _, err = dockhand(t, "clean", "jq-update", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update: removed worktree ~/Source/macports-branches/jq-update\n", "one line a branch, once done")
	require.NoDirExists(t, dir)
}

// --closed adds to what clean takes; merged branches stay in it unless
// --merged=false leaves them (the dogfood run with be3f3e06, whose clean
// --archived left three merged branches, unsaid).
func TestCleanAddsWhatItsFlagsName(t *testing.T) {
	t.Parallel()
	require.Equal(t, []model.BranchState{model.BranchMerged}, cleanStates(true, false))
	require.Equal(t, []model.BranchState{model.BranchMerged, model.BranchClosed}, cleanStates(true, true))
	require.Equal(t, []model.BranchState{model.BranchClosed}, cleanStates(false, true))
	require.Empty(t, cleanStates(false, false))
}

// On a terminal, archive asks what to do with a worktree's edits: commit
// them, where tidy's plan needs no words, or discard them, and then takes
// the worktree; an open pull request is said, never closed.
func TestArchiveAsksCommitOrDiscard(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	setenv(t, "MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	setenv(t, "MACPORTS_TREE", w.clone)

	var out, errs bytes.Buffer
	err = runFor(t, t.Context(), []string{"archive", "jq-update"}, term.Streams{In: strings.NewReader("c\n"), Out: &out, Err: &errs}.AsTerminal())
	require.NoError(t, err)
	require.Contains(t, errs.String(), "jq-update's worktree stays for now: it has uncommitted edits to textproc/jq/Portfile.\n? commit them, discard them, or keep the worktree? ")
	require.Contains(t, out.String(), "Archived jq-update, and removed its worktree;")
	require.NoDirExists(t, dir)
	require.Equal(t, "jq: update to 1.8.1", strings.TrimSpace(testsupport.Git(t, w.clone, "log", "-1", "--format=%s", "dockhand/jq-update")), "committed as tidy would")

	_, _, err = dockhand(t, "start", "scratch")
	require.NoError(t, err)
	scratch := filepath.Join(w.home, "Source", "macports-branches", "scratch")
	require.NoError(t, os.WriteFile(filepath.Join(scratch, "notes.txt"), []byte("mine\n"), 0o644))
	out.Reset()
	errs.Reset()
	err = runFor(t, t.Context(), []string{"archive", "scratch"}, term.Streams{In: strings.NewReader("d\n"), Out: &out, Err: &errs}.AsTerminal())
	require.NoError(t, err)
	require.NoDirExists(t, scratch)

	_, _, err = dockhand(t, "clean", "--merged=false")
	require.ErrorContains(t, err, "nothing to clean: --merged or --closed names what")
	_, _, err = dockhand(t, "clean", "--legacy")
	require.ErrorContains(t, err, "unknown flag: --legacy", "bridge 1 is gone")
}

// clean --delete forgets an archived branch: its Git branch, with the
// commits master lacks listed first, and its record, which status --all no
// longer lists, so its name starts a branch again (roadmap item 8).
func TestCleanDeleteForgetsAnArchivedBranch(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	setenv(t, "MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	testsupport.Git(t, dir, "commit", "-q", "-am", "jq: update to 1.8.1")
	setenv(t, "MACPORTS_TREE", w.clone)

	_, _, err = dockhand(t, "clean", "--delete", "--yes", "jq-update")
	require.ErrorContains(t, err, "jq-update is open; clean --delete forgets an archived branch")
	_, _, err = dockhand(t, "archive", "jq-update")
	require.NoError(t, err)

	_, _, err = dockhand(t, "clean", "--delete", "jq-update")
	require.ErrorContains(t, err, "nothing was forgotten: without a terminal, --yes forgets these")
	out, _, err := dockhand(t, "clean", "--delete", "--yes", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · archived\n  branch dockhand/jq-update at ")
	require.Contains(t, out, "  ! 1 commit master doesn't have, deleted with it:\n")
	require.Contains(t, out, " jq: update to 1.8.1\n")
	require.Contains(t, out, "Forgot jq-update.\n")
	require.Empty(t, testsupport.Git(t, w.clone, "branch", "--list", "dockhand/jq-update"))

	listed, _, err := dockhand(t, "status", "--all")
	require.NoError(t, err)
	require.NotContains(t, listed, "jq-update")
	_, _, err = dockhand(t, "start", "jq-update")
	require.NoError(t, err, "its name starts a branch again")
}
