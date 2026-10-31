package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// gitCounted makes the engine's git a wrapper that logs the command of
// each git process it starts, and returns a reader of those logged since,
// by command.
func gitCounted(t *testing.T, f *fixture) func() map[string]int {
	t.Helper()
	real, err := exec.LookPath("git")
	require.NoError(t, err)
	dir := t.TempDir()
	log := filepath.Join(dir, "git.log")
	wrapper := filepath.Join(dir, "git")
	// The command is the first argument after git's -c pairs.
	script := fmt.Sprintf("#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in -c) skip=1;; *) if [ -n \"$skip\" ]; then skip=; else echo \"$a\" >> %q; break; fi;; esac; done\nexec %q \"$@\"\n", log, real)
	testsupport.WriteExecutable(t, wrapper, script)
	f.options.Git = wrapper
	return func() map[string]int {
		data, _ := os.ReadFile(log)
		_ = os.Remove(log)
		counts := map[string]int{}
		for _, command := range strings.Fields(string(data)) {
			counts[command]++
		}
		return counts
	}
}

// status --all reads every branch's Git branch at once, so its git
// processes don't grow with its branches but for each worktree's own
// status: rc2's ran some ten a branch, 3,962 over dhtest's 683 (Then's
// item 6).
func TestStatusOfAllRunsAFewGitProcessesWhateverItsBranches(t *testing.T) {
	t.Parallel()
	cost := func(branches int) (map[string]int, int) {
		f := setup(t)
		counted := gitCounted(t, &f)
		e, _ := f.withPreparer(t)
		for i := range branches {
			branch, err := e.Start(t.Context(), StartRequest{Name: fmt.Sprintf("b%d", i)})
			require.NoError(t, err)
			// Each changes a port, as a branch does.
			write(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": fmt.Sprintf("name libharbor\nversion 2\nrevision %d\n", i+1)})
			testsupport.Git(t, branch.Worktree, "add", "--sparse", "devel/libharbor/Portfile")
			testsupport.Git(t, branch.Worktree, "commit", "-q", "-m", "libharbor: a change")
			if i%2 == 1 {
				_, err = e.Archive(t.Context(), branch, false)
				require.NoError(t, err)
			}
		}
		counted()
		statuses, err := e.Status(t.Context(), model.BranchOpen, model.BranchMerged, model.BranchClosed, model.BranchArchived)
		require.NoError(t, err)
		require.Len(t, statuses, branches)
		counts := counted()
		worktrees := counts["status"]
		delete(counts, "status")
		return counts, worktrees
	}
	few, fewWorktrees := cost(2)
	many, manyWorktrees := cost(8)
	require.Equal(t, few, many, "the same git processes for 2 branches and 8, but for each worktree's status")
	require.Equal(t, 2, fewWorktrees)
	require.Equal(t, 8, manyWorktrees)
}
