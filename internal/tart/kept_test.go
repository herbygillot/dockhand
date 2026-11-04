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
	require.NoError(t, KeepClone(home, "dockhand-check-run-1-tahoe-1", "dockhand-xcode-tahoe", KeptAsked))
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

// A clone kept for its failure says so until it's forgotten; one asked
// for says that.
func TestAKeptCloneSaysWhyItWasKept(t *testing.T) {
	t.Parallel()
	home := filepath.Join(t.TempDir(), ".dockhand", "tart")
	require.NoError(t, os.MkdirAll(home, 0o700))
	require.NoError(t, KeepClone(home, "dockhand-check-run-1-tahoe-1", "dockhand-xcode-tahoe", KeptFailed))
	require.NoError(t, KeepClone(home, "dockhand-check-run-2-tahoe-1", "dockhand-xcode-tahoe", KeptAsked))
	require.Equal(t, KeptFailed, KeptFor(home, "dockhand-check-run-1-tahoe-1"))
	require.Equal(t, KeptAsked, KeptFor(home, "dockhand-check-run-2-tahoe-1"))
	require.NoError(t, ForgetClone(home, "dockhand-check-run-1-tahoe-1"))
	require.Equal(t, KeptAsked, KeptFor(home, "dockhand-check-run-1-tahoe-1"), "the failure's mark goes with it")
}
