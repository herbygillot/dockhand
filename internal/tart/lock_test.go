package tart

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestImageLocksAllowReadersAndExcludeWriter(t *testing.T) {
	home := t.TempDir()
	first, err := AcquireImageRead(t.Context(), home, "base")
	require.NoError(t, err)
	defer first.Close()
	second, err := AcquireImageRead(t.Context(), home, "base")
	require.NoError(t, err)
	require.NoError(t, second.Close())

	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	defer cancel()
	_, err = AcquireImageWrite(ctx, home, "base")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded))

	require.NoError(t, first.Close())
	writer, err := AcquireImageWrite(t.Context(), home, "base")
	require.NoError(t, err)
	require.NoError(t, writer.Close())
}

// With dockhand's Tart home placed elsewhere, its locks and image records
// are beside it, and nothing lands in ~/.dockhand.
func TestStateFollowsDockhandsTartHome(t *testing.T) {
	t.Parallel()
	scratch := t.TempDir()
	canonical, err := filepath.EvalSymlinks(scratch)
	require.NoError(t, err)
	locks, err := LockDirectory(filepath.Join(scratch, "tart", "dockhand"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(locks, filepath.Join(canonical, "tart", "tart-locks")+string(filepath.Separator)), locks)
	records, err := ImageRecordDirectory(filepath.Join(scratch, "tart", "dockhand"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(records, filepath.Join(canonical, "tart", "tart-images")+string(filepath.Separator)), records)
}

// Locks live in dockhand's own directory, never inside the Tart home, and
// every spelling of one Tart home shares them.
func TestLocksLiveOutsideTheTartHomeKeyedByItsCanonicalPath(t *testing.T) {
	// Beside the home, ~/.dockhand for ~/.dockhand/tart.
	t.Parallel()
	user, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	home := filepath.Join(user, ".dockhand", "tart")
	require.NoError(t, os.MkdirAll(home, 0o700))
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(home, alias))
	direct, err := LockDirectory(home)
	require.NoError(t, err)
	aliased, err := LockDirectory(alias)
	require.NoError(t, err)
	require.Equal(t, direct, aliased)
	require.True(t, strings.HasPrefix(direct, filepath.Join(user, ".dockhand", "tart-locks")+string(filepath.Separator)), direct)

	writer, err := AcquireImageWrite(t.Context(), alias, "base")
	require.NoError(t, err)
	defer writer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	defer cancel()
	_, err = AcquireImageRead(ctx, home, "base")
	require.ErrorIs(t, err, context.DeadlineExceeded, "the alias and the path hold one lock")
	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	require.Empty(t, entries, "nothing is written inside the Tart home")
}
