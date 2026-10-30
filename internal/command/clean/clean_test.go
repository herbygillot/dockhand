package clean

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/engine"
)

// Once a command's own work is done, it starts automatic cleanup apart from
// itself when a day has passed since the last (decision 36); clean
// --automatic is that pass, and the next command the same day starts none.
// cleanup.automatic = false starts none at all.
func TestACommandStartsCleanupWhenItIsDue(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	var started []engine.Options
	cmdtest.Hosted(t).StartCleanup = func(options engine.Options) error {
		started = append(started, options)
		return nil
	}

	_, _, err := cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Len(t, started, 1, "never cleaned")
	require.Equal(t, filepath.Join(w.Home, ".dockhand", "dockhand.db"), started[0].Database)

	out, _, err := cmdtest.Dockhand(t, "clean", "--automatic")
	require.NoError(t, err)
	require.Contains(t, out, " cleaning up: 24h0m0s since the last\n")
	require.Contains(t, out, "  removed 0 items\n")
	require.Len(t, started, 1, "clean doesn't start another")
	out, _, err = cmdtest.Dockhand(t, "clean", "--automatic")
	require.NoError(t, err)
	require.Empty(t, out, "not due again the same day")

	_, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Len(t, started, 1, "not again the same day")

	require.NoError(t, os.Remove(cmdtest.CleanupStamp(t, w.Home)))
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"), []byte("[cleanup]\nautomatic = false\n"), 0o644))
	_, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Len(t, started, 1, "turned off")
}

// A fork that refused this Mac is said in one line, its branches not
// counted as kept, where "kept 515" read as alarming after a reboot left
// no SSH key loaded.
func TestAnUnreachedForkIsOneLine(t *testing.T) {
	t.Parallel()
	steps := []engine.CleanStep{{Kind: engine.CleanFork, Kept: "it could not be read", Unreached: true}, {Kind: engine.CleanFork, Kept: "it could not be read", Unreached: true}, {Kind: engine.CleanWorktree, Done: true}}
	require.Equal(t, "Removed 1 worktree; your fork couldn't be reached (is your SSH key loaded? ssh-add -l lists the agent's keys), so 2 fork branches weren't looked at.",
		cleanedWords([]engine.CleanBranch{{Steps: steps}}, nil))
}
