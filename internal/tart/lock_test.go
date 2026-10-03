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
	t.Setenv("HOME", t.TempDir())
	scratch := t.TempDir()
	t.Setenv("DOCKHAND_TART_HOME", filepath.Join(scratch, "tart", "dockhand"))
	canonical, err := filepath.EvalSymlinks(scratch)
	require.NoError(t, err)
	locks, err := LockDirectory(filepath.Join(scratch, "tart", "tart"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(locks, filepath.Join(canonical, "tart", "tart-locks")+string(filepath.Separator)), locks)
	records, err := ImageRecordDirectory(filepath.Join(scratch, "tart", "dockhand"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(records, filepath.Join(canonical, "tart", "tart-images")+string(filepath.Separator)), records)
	user, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoDirExists(t, filepath.Join(user, ".dockhand"))
}

// Locks live in dockhand's own directory, never inside the Tart home, and
// every spelling of one Tart home shares them.
func TestLocksLiveOutsideTheTartHomeKeyedByItsCanonicalPath(t *testing.T) {
	// ~/.dockhand's, whatever dockhand's Tart home the environment names,
	// as the quick stage's does (the M1's run at 10aac0c3, A0).
	t.Setenv("DOCKHAND_TART_HOME", "")
	home := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(home, alias))
	direct, err := LockDirectory(home)
	require.NoError(t, err)
	aliased, err := LockDirectory(alias)
	require.NoError(t, err)
	require.Equal(t, direct, aliased)
	user, err := os.UserHomeDir()
	require.NoError(t, err)
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
