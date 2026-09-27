package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
)

// Once a command's own work is done, it starts automatic cleanup apart from
// itself when a day has passed since the last (decision 36); clean
// --automatic is that pass, and the next command the same day starts none.
// cleanup.automatic = false starts none at all.
func TestACommandStartsCleanupWhenItIsDue(t *testing.T) {
	w := newWorld(t)
	var started []engine.Options
	startCleanup = func(options engine.Options) error {
		started = append(started, options)
		return nil
	}
	t.Cleanup(func() { startCleanup = func(engine.Options) error { return nil } })

	_, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Len(t, started, 1, "never cleaned")
	require.Equal(t, filepath.Join(w.home, ".dockhand", "dockhand.db"), started[0].Database)

	out, _, err := dockhand(t, "clean", "--automatic")
	require.NoError(t, err)
	require.Contains(t, out, " cleaning up: 24h0m0s since the last\n")
	require.Contains(t, out, "  removed 0 items\n")
	require.Len(t, started, 1, "clean doesn't start another")
	out, _, err = dockhand(t, "clean", "--automatic")
	require.NoError(t, err)
	require.Empty(t, out, "not due again the same day")

	_, _, err = dockhand(t, "status")
	require.NoError(t, err)
	require.Len(t, started, 1, "not again the same day")

	require.NoError(t, os.Remove(filepath.Join(w.home, ".dockhand", "cleanup.stamp")))
	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".dockhand", "config.toml"), []byte("[cleanup]\nautomatic = false\n"), 0o644))
	_, _, err = dockhand(t, "status")
	require.NoError(t, err)
	require.Len(t, started, 1, "turned off")
}
