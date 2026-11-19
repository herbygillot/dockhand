package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
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
			testsupport.WriteFiles(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": fmt.Sprintf("name libharbor\nversion 2\nrevision %d\n", i+1)})
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

// OpenWork's stale check uses one rev-list across branches with distinct
// bases; ordinary branches still use one batched net-tree comparison. At
// 6c0a32bb the same 20-to-200 fixture grew from 28 to 208 Git processes.
func TestOpenWorkUsesOneHistoryWalkFor200DistinctBases(t *testing.T) {
	t.Parallel()
	f := setup(t)
	counted := gitCounted(t, &f)
	e := f.open(t)
	const branches = 200
	bases := make([]string, branches)
	heads := make([]string, branches)
	for i := range branches {
		testsupport.WriteFiles(t, f.upstream, map[string]string{"devel/libharbor/Portfile": fmt.Sprintf("name libharbor\nversion %d\n", i+3)})
		testsupport.Git(t, f.upstream, "commit", "-q", "-am", "libharbor: update")
		bases[i] = testsupport.Git(t, f.upstream, "rev-parse", "HEAD")
	}
	testsupport.Git(t, f.clone, "fetch", "origin", "master")
	for i, base := range bases {
		testsupport.Git(t, f.clone, "checkout", "-q", "--detach", base)
		testsupport.WriteFiles(t, f.clone, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
		testsupport.Git(t, f.clone, "commit", "-q", "-am", "jq: update")
		heads[i] = testsupport.Git(t, f.clone, "rev-parse", "HEAD")
	}
	addBranches := func(from, to int) {
		t.Helper()
		changes := make([]git.RefChange, 0, to-from)
		for i := from; i < to; i++ {
			name := fmt.Sprintf("refs/heads/dockhand/cost-%03d", i)
			changes = append(changes, git.RefChange{Name: name, Desired: git.RefValue{Exists: true, Object: heads[i]}})
		}
		require.NoError(t, e.Repo.UpdateRefs(t.Context(), changes))
		require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
			for i := from; i < to; i++ {
				name := fmt.Sprintf("dockhand/cost-%03d", i)
				branch := model.Branch{ID: model.BranchID(store.NewID("br")), Repository: e.Repository, Name: name,
					Base: model.ObjectID(bases[i]), Worktree: filepath.Join(f.options.Worktrees, name), Managed: true,
					State: model.BranchOpen, CreatedAt: at, Origin: model.OriginPerson}
				if err := tx.AddBranch(branch); err != nil {
					return err
				}
			}
			return nil
		}))
	}
	measure := func(want int) map[string]int {
		t.Helper()
		counted() // fixture construction isn't part of OpenWork.
		work, err := e.OpenWork(t.Context())
		require.NoError(t, err)
		require.Len(t, work.branches, want)
		return counted()
	}
	total := func(counts map[string]int) int {
		var n int
		for _, count := range counts {
			n += count
		}
		return n
	}
	const fewer = 20
	addBranches(0, fewer)
	few := measure(fewer)
	addBranches(fewer, branches)
	many := measure(branches)
	t.Logf("OpenWork Git processes: %d for %d branches, %d for %d branches (200 distinct bases)", total(few), fewer, total(many), branches)
	require.Equal(t, few, many, "Git process count doesn't grow with branch count")
	require.LessOrEqual(t, total(many), 10)
}
