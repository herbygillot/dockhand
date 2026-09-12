package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/app"
	"github.com/herbygillot/dockhand/v2/internal/cli"
	"github.com/stretchr/testify/require"
)

func TestLockDirectoryFlagAndHelp(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	root, err := cli.NewRoot(app.Config{})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".dockhand", "lock"), root.PersistentFlags().Lookup("lock-dir").DefValue)
	require.Nil(t, root.PersistentFlags().Lookup("lockfile"))

	working := t.TempDir()
	t.Chdir(working)
	for _, args := range [][]string{
		{"--lock-dir", "custom", "status", "--help"},
		{"status", "--lock-dir=custom", "--help"},
		{"status", "-L", "custom", "--help"},
	} {
		var output bytes.Buffer
		err := cli.Run(t.Context(), args, cli.Streams{Out: &output, Err: &output}, app.Config{Repository: "/missing/repository"})
		require.NoError(t, err)
		require.Contains(t, output.String(), "Lock directory: "+filepath.Join(working, "custom"))
		require.NoDirExists(t, filepath.Join(working, "custom"))
	}
	for _, args := range [][]string{{"--lock-dir=", "status"}, {"--lockfile", "old", "status"}} {
		var output bytes.Buffer
		err := cli.Run(t.Context(), args, cli.Streams{Out: &output, Err: &output}, app.Config{})
		require.Error(t, err)
	}
	var output bytes.Buffer
	require.NoError(t, cli.Run(t.Context(), []string{"completion", "zsh"}, cli.Streams{Out: &output, Err: &output}, app.Config{LockDir: filepath.Join(working, "completion")}))
	require.NoDirExists(t, filepath.Join(working, "completion"))
}
