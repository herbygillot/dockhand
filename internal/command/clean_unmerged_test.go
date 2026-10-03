package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// archive takes a branch's worktree where it holds nothing the branch's
// commits don't, in one step (the command-line UX review, §5), keeping
// the Git branch, and path checks it out again. One with edits of its own
// stays, saying why, unless --discard; and clean --archived still takes
// one archived before.
func TestArchiveTakesTheWorktreeAndPathBringsItBack(t *testing.T) {
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
	head := strings.TrimSpace(testsupport.Git(t, dir, "rev-parse", "HEAD"))
	t.Setenv("MACPORTS_TREE", w.clone)

	// A worktree with edits of its own stays, and says why.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine\n"), 0o644))
	archived, _, err := dockhand(t, "archive", "jq-update")
	require.NoError(t, err)
	require.Contains(t, archived, "Archived jq-update; its worktree stays, since it has untracked files: notes.txt. dockhand archive --discard jq-update removes it, edits and all.\n")
	require.DirExists(t, dir)
	out, _, err := dockhand(t, "clean")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out, "plain clean is merged branches only")

	archived, _, err = dockhand(t, "archive", "--discard", "jq-update")
	require.NoError(t, err)
	require.Contains(t, archived, "Archived jq-update, and removed its worktree; its Git branch and pull request stay. dockhand archive --undo jq-update brings it back.\n")
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

	// One archived before archive took worktrees is clean --archived's.
	out, _, err = dockhand(t, "clean", "--archived", "jq-update")
	require.NoError(t, err)
	require.Equal(t, "jq-update (archived)\n  remove   worktree ~/Source/macports-branches/jq-update\n"+
		"  keep     branch dockhand/jq-update: dockhand path jq-update checks it out again\n"+
		"Nothing was removed; --yes removes these.\n", out)
	out, _, err = dockhand(t, "clean", "--archived", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update: removed worktree ~/Source/macports-branches/jq-update\n", "one line a branch, once done")
	require.NoDirExists(t, dir)
}

// --closed and --archived add to what clean takes; merged branches stay in
// it unless --merged=false leaves them (the dogfood run with be3f3e06,
// whose clean --archived left three merged branches, unsaid).
func TestCleanAddsWhatItsFlagsName(t *testing.T) {
	require.Equal(t, []model.BranchState{model.BranchMerged}, cleanStates(true, false, false))
	require.Equal(t, []model.BranchState{model.BranchMerged, model.BranchArchived}, cleanStates(true, false, true))
	require.Equal(t, []model.BranchState{model.BranchMerged, model.BranchClosed, model.BranchArchived}, cleanStates(true, true, true))
	require.Equal(t, []model.BranchState{model.BranchArchived}, cleanStates(false, false, true))
	require.Empty(t, cleanStates(false, false, false))
}

// clean --legacy says what it does with each branch from before v3: one
// master has goes, with your fork's holding the same commit; one master
// superseded is for a look; the rest are adopt's.
func TestCleanSaysWhatItDoesWithBranchesFromBeforeV3(t *testing.T) {
	plans := []engine.LegacyBranch{
		{Name: "dockhand/bump/jq-9c1d", Kind: engine.LegacySuperseded, Detail: "master has jq at 1.8.1, where the branch took 1.7.1 to 1.8.0"},
		{Name: "dockhand/bump/libharbor-4f2a", Kind: engine.LegacyOnMaster, Detail: `master has the same change as its "libharbor: update to 2"`, Fork: "ada/macports-ports:dockhand/bump/libharbor-4f2a"},
		{Name: "dockhand/bump/newport-77aa", Kind: engine.LegacyUnfinished, Detail: "takes newport from 1 to 2, which master still has at 1"},
		{Name: "dockhand/bump/xplr-atgg", Kind: engine.LegacyOnMaster, Detail: `master has the same change as its "xplr: update to 1.0"`, ForkOnly: true, Fork: "ada/macports-ports:dockhand/bump/xplr-atgg"},
	}
	var out bytes.Buffer
	require.Equal(t, 3, writeLegacy(&out, plans, false))
	require.Equal(t, "Branches from before v3 (dockhand/bump/…):\n"+
		"  look     dockhand/bump/jq-9c1d: master has jq at 1.8.1, where the branch took 1.7.1 to 1.8.0; git branch -D dockhand/bump/jq-9c1d removes it once you've looked\n"+
		"  remove   dockhand/bump/libharbor-4f2a: master has the same change as its \"libharbor: update to 2\"\n"+
		"  remove   ada/macports-ports:dockhand/bump/libharbor-4f2a, which holds the same commit\n"+
		"  keep     dockhand/bump/newport-77aa: takes newport from 1 to 2, which master still has at 1; dockhand adopt dockhand/bump/newport-77aa takes it up\n"+
		"  remove   ada/macports-ports:dockhand/bump/xplr-atgg (on your fork only): master has the same change as its \"xplr: update to 1.0\"\n", out.String())
	require.Zero(t, writeLegacy(&out, nil, false))
}

// On a terminal, archive asks what to do with a worktree's edits: commit
// them, where tidy's plan needs no words, or discard them, and then takes
// the worktree; an open pull request is said, never closed. clean
// --legacy names what to clean by itself.
func TestArchiveAsksCommitOrDiscard(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", w.clone)

	var out, errs bytes.Buffer
	err = Run(t.Context(), []string{"archive", "jq-update"}, Streams{In: strings.NewReader("c\n"), Out: &out, Err: &errs, interactive: true})
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
	err = Run(t.Context(), []string{"archive", "scratch"}, Streams{In: strings.NewReader("d\n"), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.NoDirExists(t, scratch)

	_, _, err = dockhand(t, "clean", "--legacy", "--merged=false")
	require.NoError(t, err, "--legacy names what to clean")
	_, _, err = dockhand(t, "clean", "--merged=false")
	require.ErrorContains(t, err, "nothing to clean: --merged, --closed, or --legacy names what")
}
