package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/cli"
	"github.com/stretchr/testify/require"
)

func TestDatabaseFlagAndHelp(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	root, err := cli.NewRoot(app.Config{})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".dockhand", "state.db"), root.PersistentFlags().Lookup("db").DefValue)
	require.Nil(t, root.PersistentFlags().Lookup("lockfile"))

	working := t.TempDir()
	t.Chdir(working)
	for _, args := range [][]string{
		{"--db", "custom", "status", "--help"},
		{"status", "--db=custom", "--help"},
	} {
		var output bytes.Buffer
		err := cli.Run(t.Context(), args, cli.Streams{Out: &output, Err: &output}, app.Config{Repository: "/missing/repository"})
		require.NoError(t, err)
		require.Contains(t, output.String(), "State database: "+filepath.Join(working, "custom"))
		require.NoDirExists(t, filepath.Join(working, "custom"))
	}
	for _, args := range [][]string{{"--db=file:/tmp/state.db", "status"}, {"--db=:memory:", "status"}, {"--db=", "status"}, {"--lock-dir", "old", "status"}, {"--lockfile", "old", "status"}, {"-L", "old", "status"}} {
		var output bytes.Buffer
		err := cli.Run(t.Context(), args, cli.Streams{Out: &output, Err: &output}, app.Config{})
		require.Error(t, err)
	}
	var output bytes.Buffer
	require.NoError(t, cli.Run(t.Context(), []string{"completion", "zsh"}, cli.Streams{Out: &output, Err: &output}, app.Config{DBPath: filepath.Join(working, "completion")}))
	require.NoDirExists(t, filepath.Join(working, "completion"))
	// The main help shows the logo with the build's version beneath it.
	output.Reset()
	require.NoError(t, cli.Run(t.Context(), []string{"--help"}, cli.Streams{Out: &output, Err: &output}, app.Config{}))
	require.Contains(t, output.String(), "|_|\\__,_|_| |_|\\__,_|\nversion "+root.Version+"\n\nDockhand prepares")
}
