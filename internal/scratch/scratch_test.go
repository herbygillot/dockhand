package scratch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The package's own root lives under os.TempDir; tests point it at their
// own directory so a parallel test binary's roots are not in the picture.
// It returns the directory as os.TempDir reports it and with symlinks
// resolved, as a root's path is.
func withTempDir(t *testing.T) (parent, resolved string) {
	t.Helper()
	parent = t.TempDir()
	t.Setenv("TMPDIR", parent)
	resolved, err := filepath.EvalSymlinks(parent)
	require.NoError(t, err)
	require.NoError(t, Close())
	t.Cleanup(func() { _ = Close() })
	return parent, resolved
}

func TestDirLivesUnderOneLockedRootRemovedOnClose(t *testing.T) {
	_, parent := withTempDir(t)
	a, err := Dir("workspace-")
	require.NoError(t, err)
	b, err := Dir("overlay-")
	require.NoError(t, err)
	require.Equal(t, filepath.Dir(a), filepath.Dir(b), "one root per process")
	root := filepath.Dir(a)
	require.Equal(t, parent, filepath.Dir(root))
	require.True(t, filepath.Base(root)[0] != '.', "the root is visible once locked")
	// The lock is held: a second handle cannot take it.
	other, err := os.OpenFile(filepath.Join(root, lockName), os.O_RDWR, 0)
	require.NoError(t, err)
	defer other.Close()
	require.ErrorIs(t, syscall.Flock(int(other.Fd()), syscall.LOCK_EX|syscall.LOCK_NB), syscall.EWOULDBLOCK)
	stale, err := Stale(time.Time{})
	require.NoError(t, err)
	require.Empty(t, stale, "the process's own root is never stale")
	require.NoError(t, Close())
	require.NoDirExists(t, root)
	c, err := Dir("again-")
	require.NoError(t, err)
	require.NotEqual(t, root, filepath.Dir(c), "a later Dir makes a new root")
}

func TestSweepRemovesTheRootsOfDeadProcessesAndKeepsLiveOnes(t *testing.T) {
	parent, _ := withTempDir(t)
	own, err := Dir("mine-")
	require.NoError(t, err, "the root exists first; its creation sweeps too")
	dead := filepath.Join(parent, prefix+"dead")
	require.NoError(t, os.MkdirAll(filepath.Join(dead, "workspace", "devel"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dead, lockName), nil, 0600))
	live := filepath.Join(parent, prefix+"live")
	require.NoError(t, os.MkdirAll(live, 0700))
	held, err := os.OpenFile(filepath.Join(live, lockName), os.O_CREATE|os.O_RDWR, 0600)
	require.NoError(t, err)
	defer held.Close()
	require.NoError(t, syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	// A hidden root being made right now: its lock file exists but is not
	// yet locked, the window a sweeper must not take it in.
	fresh := filepath.Join(parent, "."+prefix+"fresh")
	require.NoError(t, os.MkdirAll(fresh, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(fresh, lockName), nil, 0600))
	old := filepath.Join(parent, "."+prefix+"old")
	require.NoError(t, os.MkdirAll(old, 0700))
	past := time.Now().Add(-2 * abandoned)
	require.NoError(t, os.Chtimes(old, past, past))
	require.NoError(t, os.WriteFile(filepath.Join(parent, prefix+"file"), nil, 0600), "a file with the prefix is not a root")
	// Directories an earlier build made outside a run root carry no lock;
	// only their age says nothing is using them.
	legacyOld := filepath.Join(parent, legacy+"overlay-1")
	require.NoError(t, os.MkdirAll(legacyOld, 0700))
	require.NoError(t, os.Chtimes(legacyOld, past, past))
	legacyNew := filepath.Join(parent, legacy+"overlay-2")
	require.NoError(t, os.MkdirAll(legacyNew, 0700))

	stale, err := Stale(time.Time{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{dead, old}, stale, "no legacy time, no legacy directories")
	require.DirExists(t, dead, "listing removes nothing")
	stale, err = Stale(time.Now().Add(-time.Minute))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{dead, old, legacyOld}, stale)

	removed, err := Sweep(time.Now().Add(-time.Minute))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{dead, old, legacyOld}, removed)
	require.NoDirExists(t, dead)
	require.NoDirExists(t, old)
	require.NoDirExists(t, legacyOld)
	require.DirExists(t, legacyNew, "a legacy directory newer than the threshold may be in use")
	require.DirExists(t, live, "a locked root belongs to a running process")
	require.DirExists(t, fresh, "a root still being made is left alone")
	require.DirExists(t, own)
}

func TestARootRemovedFromUnderTheProcessIsReplaced(t *testing.T) {
	withTempDir(t)
	first, err := Root()
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(first))
	second, err := Dir("after-")
	require.NoError(t, err)
	require.NotEqual(t, first, filepath.Dir(second))
	require.DirExists(t, second)
}

func TestRootCreationSweepsDeadRoots(t *testing.T) {
	parent, _ := withTempDir(t)
	dead := filepath.Join(parent, prefix+"dead")
	require.NoError(t, os.MkdirAll(dead, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dead, lockName), nil, 0600))
	_, err := Root()
	require.NoError(t, err)
	require.NoDirExists(t, dead)
}

// Processes starting together sweep while others are creating their roots.
// A root is published only once locked, and a hidden root is judged by age
// alone, so no process ends up with a root a sweeper took from under it:
// every root the children leave still holds its lock file.
func TestConcurrentRootCreationLeavesEveryRootLocked(t *testing.T) {
	if os.Getenv("SCRATCH_CHILD") == "1" {
		_, err := Dir("child-")
		require.NoError(t, err)
		return // the root stays, unlocked by exit, for the parent to sweep
	}
	parent, _ := withTempDir(t)
	const children = 24
	var commands []*exec.Cmd
	for range children {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestConcurrentRootCreationLeavesEveryRootLocked$")
		cmd.Env = append(os.Environ(), "SCRATCH_CHILD=1", "TMPDIR="+parent)
		require.NoError(t, cmd.Start())
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		require.NoError(t, cmd.Wait())
	}
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	roots := 0
	for _, entry := range entries {
		require.True(t, strings.HasPrefix(entry.Name(), prefix), "a hidden or foreign entry: %s", entry.Name())
		require.FileExists(t, filepath.Join(parent, entry.Name(), lockName), "a root lost its lock to a sweeper")
		roots++
	}
	require.Positive(t, roots, "the children left roots for the parent")
	removed, err := Sweep(time.Time{})
	require.NoError(t, err)
	require.Len(t, removed, roots)
}
