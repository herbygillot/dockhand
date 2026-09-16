package filelock

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPathHashesKeysIntoStableLockNames(t *testing.T) {
	first := Path("/locks", "refs/heads/feature")
	require.Equal(t, first, Path("/locks", "refs/heads/feature"))
	require.NotEqual(t, first, Path("/locks", "refs/heads/other"))
	require.Equal(t, "/locks", filepath.Dir(first))
	require.True(t, strings.HasSuffix(first, ".lock"))
	require.Len(t, filepath.Base(first), 64+len(".lock"))
}

func TestWithHoldsTheLockAroundTheCallbackAndKeepsTheFile(t *testing.T) {
	path := Path(filepath.Join(t.TempDir(), "locks"), "key")
	var inside *os.File
	err := With(t.Context(), path, Exclusive, func(ctx context.Context, file *os.File) error {
		inside = file
		blocked, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, err := Acquire(blocked, path, Exclusive)
		require.ErrorIs(t, err, context.DeadlineExceeded, "the callback holds the lock")
		_, err = TryExisting(ctx, path, Shared)
		require.ErrorIs(t, err, ErrBusy)
		return nil
	})
	require.NoError(t, err)
	require.NotNil(t, inside)
	require.FileExists(t, path, "the lock file stays so waiters share one inode")
	again, err := Acquire(t.Context(), path, Exclusive)
	require.NoError(t, err, "the lock is released after the callback")
	again.Close()
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, With(canceled, path, Exclusive, func(context.Context, *os.File) error { t.Fatal("must not run"); return nil }), context.Canceled)
}

func TestAcquireRefusesSymlinkedLockFiles(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	require.NoError(t, os.WriteFile(target, nil, 0600))
	link := filepath.Join(root, "link.lock")
	require.NoError(t, os.Symlink(target, link))
	_, err := Acquire(t.Context(), link, Exclusive)
	require.Error(t, err)
}
