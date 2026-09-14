package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/cli"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/stretchr/testify/require"
)

func TestDatabaseMaintenanceNeedsNoRepositoryOrProvider(t *testing.T) {
	root := t.TempDir()
	config := app.Config{DBPath: filepath.Join(root, "state.db"), Repository: "/missing/repository", GitExecutable: "/missing/git", TclExecutable: "/missing/tcl"}
	store, err := sqlite.Open(t.Context(), config.DBPath, sqlite.Options{})
	require.NoError(t, err)
	_, err = store.RegisterRepository(t.Context(), "/missing/repository")
	require.NoError(t, err)
	defer store.Close()
	var output bytes.Buffer
	require.NoError(t, cli.Run(t.Context(), []string{"db", "check", "--json"}, cli.Streams{Out: &output, Err: &output}, config))
	require.JSONEq(t, `{"Valid":true}`, output.String())
	output.Reset()
	destination := filepath.Join(root, "backup.db")
	require.NoError(t, cli.Run(t.Context(), []string{"db", "backup", destination, "--json"}, cli.Streams{Out: &output, Err: &output}, config))
	var result state.Backup
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.Positive(t, result.Bytes)
	output.Reset()
	require.NoError(t, cli.Run(t.Context(), []string{"--db", destination, "db", "check"}, cli.Streams{Out: &output, Err: &output}, config))
	require.Contains(t, output.String(), "integrity: ok")
	output.Reset()
	require.Error(t, cli.Run(t.Context(), []string{"db", "backup", destination}, cli.Streams{Out: &output, Err: &output}, config))
	missing := filepath.Join(root, "missing", "state.db")
	for _, args := range [][]string{{"--db", missing, "db", "check"}, {"--db", missing, "db", "backup", filepath.Join(root, "never.db")}} {
		output.Reset()
		require.ErrorIs(t, cli.Run(t.Context(), args, cli.Streams{Out: &output, Err: &output}, config), state.ErrNoDatabase)
	}
	require.NoDirExists(t, filepath.Dir(missing))
	require.NoFileExists(t, filepath.Join(root, "never.db"))
}

func TestGCDryRunAndEmptyRepositoryDoNotInitializeState(t *testing.T) {
	root := t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput()
	require.NoError(t, err, "%s", out)
	config := app.Config{Repository: root, DBPath: filepath.Join(root, "missing", "state.db")}
	for _, args := range [][]string{{"gc", "--dry-run"}, {"gc"}, {"gc", "--dry-run", "--older-than", "0", "--json"}} {
		var output bytes.Buffer
		require.NoError(t, cli.Run(t.Context(), args, cli.Streams{Out: &output, Err: &output}, config))
		require.NoDirExists(t, filepath.Dir(config.DBPath))
	}
	var output bytes.Buffer
	require.Error(t, cli.Run(t.Context(), []string{"gc", "--older-than=-1h"}, cli.Streams{Out: &output, Err: &output}, config))
	require.NoDirExists(t, filepath.Dir(config.DBPath))
	// Dry-run must open an existing DB read-only, without registering this checkout.
	store, err := sqlite.Open(t.Context(), config.DBPath, sqlite.Options{})
	require.NoError(t, err)
	require.NoError(t, store.Close())
	before, err := os.ReadFile(config.DBPath)
	require.NoError(t, err)
	output.Reset()
	require.NoError(t, cli.Run(t.Context(), []string{"gc", "--dry-run"}, cli.Streams{Out: &output, Err: &output}, config))
	after, err := os.ReadFile(config.DBPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoDirExists(t, filepath.Join(filepath.Dir(config.DBPath), "artifacts"))
}
