package tart

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A kept clone is marked beside the Tart home, with when, until it's
// forgotten.
func TestAKeptCloneIsMarkedUntilForgotten(t *testing.T) {
	t.Parallel()
	home := filepath.Join(t.TempDir(), ".dockhand", "tart")
	require.NoError(t, os.MkdirAll(home, 0o700))
	_, kept := KeptAt(home, "dockhand-check-run-1-tahoe-1")
	require.False(t, kept)
	require.NoError(t, KeepClone(home, "dockhand-check-run-1-tahoe-1", "dockhand-xcode-tahoe"))
	at, kept := KeptAt(home, "dockhand-check-run-1-tahoe-1")
	require.True(t, kept)
	require.WithinDuration(t, time.Now(), at, time.Minute)
	image, kept := KeptFrom(home, "dockhand-check-run-1-tahoe-1")
	require.True(t, kept)
	require.Equal(t, "dockhand-xcode-tahoe", image)
	require.NoError(t, ForgetClone(home, "dockhand-check-run-1-tahoe-1"))
	require.NoError(t, ForgetClone(home, "dockhand-check-run-1-tahoe-1"), "forgetting twice is fine")
	_, kept = KeptAt(home, "dockhand-check-run-1-tahoe-1")
	require.False(t, kept)
}
