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
