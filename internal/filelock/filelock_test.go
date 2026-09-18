package filelock

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLockIsHeldBySurvivingChild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation.lock")
	file, err := Acquire(t.Context(), path, Exclusive)
	require.NoError(t, err)
	command := exec.Command("/bin/sh", "-c", "read line || true")
	command.ExtraFiles = []*os.File{file}
	input, err := command.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	require.NoError(t, file.Close())
	t.Cleanup(func() { input.Close(); command.Wait() })
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = Acquire(ctx, path, Exclusive)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, input.Close())
	require.NoError(t, command.Wait())
	next, err := Acquire(t.Context(), path, Exclusive)
	require.NoError(t, err)
	require.NoError(t, next.Close())
}

func TestAcquireRejectsUnknownModeBeforeCreatingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "operation.lock")
	_, err := Acquire(t.Context(), path, Mode(99))
	require.ErrorContains(t, err, "invalid mode")
	_, err = os.Stat(filepath.Dir(path))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestTryExistingDoesNotCreatePathsAndSkipsBusyLocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "lock")
	_, err := TryExisting(t.Context(), path, Exclusive)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoDirExists(t, filepath.Dir(path))
	held, err := Acquire(t.Context(), path, Exclusive)
	require.NoError(t, err)
	defer held.Close()
	_, err = TryExisting(t.Context(), path, Exclusive)
	require.ErrorIs(t, err, ErrBusy)
	require.NoError(t, held.Close())
	next, err := TryExisting(t.Context(), path, Exclusive)
	require.NoError(t, err)
	require.NoError(t, next.Close())
}

// A lock a holder is about to release is not busy: the try outlasts the
// moment a forked child keeps an inherited descriptor alive before it execs.
func TestTryExistingOutlastsAForkedChildButNotAHolder(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "lock")
	held, err := Acquire(t.Context(), path, Exclusive)
	require.NoError(t, err)
	go func() {
		time.Sleep(forkGrace / 5)
		held.Close()
	}()
	started := time.Now()
	next, err := TryExisting(t.Context(), path, Exclusive)
	require.NoError(t, err, "released within the grace period")
	require.Less(t, time.Since(started), forkGrace)
	defer next.Close()
	started = time.Now()
	_, err = TryExisting(t.Context(), path, Exclusive)
	require.ErrorIs(t, err, ErrBusy, "a holder that keeps the lock is reported")
	require.GreaterOrEqual(t, time.Since(started), forkGrace)
	// The child window is real: with commands spawning, a lock closed a moment
	// ago is still seen busy by a try that does not wait.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for range 4 {
		go func() {
			for ctx.Err() == nil {
				_ = exec.CommandContext(ctx, "/usr/bin/true").Run()
			}
		}()
	}
	probe := filepath.Join(t.TempDir(), "probe")
	for range 200 {
		file, err := Acquire(ctx, probe, Exclusive)
		require.NoError(t, err)
		require.NoError(t, file.Close())
		got, err := TryExisting(ctx, probe, Exclusive)
		require.NoError(t, err, "a released lock is acquired despite forks in flight")
		require.NoError(t, got.Close())
	}
}
