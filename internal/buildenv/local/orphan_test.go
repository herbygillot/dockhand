package local

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv/starter"
	"github.com/herbygillot/dockhand/internal/filelock"
)

// A starter forked holding the launch lock, its dockhand gone before it
// recorded itself, is a build the next check waits for: the lock is found
// from the directory, from the fork, not from the starter's record (Codex's
// review of #193 steps 5 and 6, finding 5).
func TestAStarterIsSeenFromItsForkBeforeItRecordsItself(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, localDirectory+"probe")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	prefix := filepath.Join(root, "prefix")
	owned, err := json.Marshal(owner{PID: 2147483000, Start: "gone", Check: "check-12", Prefix: prefix})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "owner.json"), owned, 0o644))
	path := filepath.Join(root, "attempt.lock")
	file, err := filelock.Acquire(t.Context(), path, filelock.Exclusive)
	require.NoError(t, err)
	defer file.Close()
	self, err := os.Executable()
	require.NoError(t, err)
	cmd, err := starter.Start(starter.Spec{
		Program:   starter.Program{Path: "/bin/sh", Args: []string{"-c", `kill -STOP $$; exec "$@"`, "sh", self}},
		Directory: dir, Lock: file, Signal: syscall.SIGTERM, Grace: time.Second, Command: []string{"/bin/sleep", "60"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	require.NoError(t, file.Close())
	require.Eventually(t, func() bool {
		_, err := filelock.TryAcquire(path)
		return errors.Is(err, filelock.ErrBusy)
	}, 5*time.Second, time.Millisecond)
	_, found, err := starter.ReadRecord(dir)
	require.NoError(t, err)
	require.False(t, found, "the starter hasn't recorded itself")
	l := &Local{Temp: root}
	require.Len(t, l.orphans(prefix), 1, "the next check waits for it")
	require.Empty(t, l.left(), "and it isn't offered as a leftover")
	require.True(t, starter.Building(context.Background(), dir))
}
