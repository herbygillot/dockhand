package cli_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/cli"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/workflow"
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

func TestOldSchemaStatusExplainsDatabaseOnlyMigration(t *testing.T) {
	root := t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput()
	require.NoError(t, err, "%s", out)
	path := filepath.Join(root, "old state.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	defer db.Close()
	schema, err := os.ReadFile("../state/sqlite/migrations/001.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(schema) + `PRAGMA application_id=0x44484e44; PRAGMA user_version=1;
 INSERT INTO repositories(id,common_dir,created_at) VALUES('preserved','/old/repository',1);`)
	require.NoError(t, err)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	config := app.Config{DBPath: path, Repository: root, TclExecutable: "/missing/tcl"}
	var required int
	for _, args := range [][]string{{"status"}, {"status", "--json"}, {"gc", "--dry-run"}} {
		var stdout, stderr bytes.Buffer
		err = cli.Run(t.Context(), args, cli.Streams{Out: &stdout, Err: &stderr}, config)
		require.ErrorIs(t, err, state.ErrSchema)
		if args[0] == "status" {
			require.Empty(t, stdout.String())
		}
		var migration *state.MigrationRequiredError
		require.ErrorAs(t, err, &migration)
		require.Equal(t, 1, migration.Current)
		required = migration.Required
		require.Greater(t, required, 1)
		require.Contains(t, err.Error(), "read-only")
		require.Contains(t, err.Error(), "dockhand db migrate")
		require.Contains(t, err.Error(), "same --db option")
		require.Contains(t, err.Error(), "dockhand db backup")
	}
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "read-only inspection must not migrate")
	require.NoDirExists(t, filepath.Join(root, "artifacts"))
	config.Repository = "/missing/repository"
	config.GitExecutable = "/missing/git"
	config.Tart.Executable = "/missing/tart"
	backup := filepath.Join(root, "backup.db")
	for _, args := range [][]string{{"db", "check"}, {"db", "backup", backup}} {
		var output bytes.Buffer
		require.NoError(t, cli.Run(t.Context(), args, cli.Streams{Out: &output, Err: &output}, config))
	}
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, 1, version)
	for range 2 {
		var output bytes.Buffer
		require.NoError(t, cli.Run(t.Context(), []string{"db", "migrate", "--json"}, cli.Streams{Out: &output, Err: &output}, config))
		require.JSONEq(t, `{"Current":true}`, output.String())
		require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
		require.Equal(t, required, version)
	}
	var common string
	require.NoError(t, db.QueryRow("SELECT common_dir FROM repositories WHERE id='preserved'").Scan(&common))
	require.Equal(t, "/old/repository", common)
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM jobs").Scan(&count))
	require.Zero(t, count)
	require.NoDirExists(t, filepath.Join(root, "artifacts"))
	preserved, err := sql.Open("sqlite", backup)
	require.NoError(t, err)
	defer preserved.Close()
	require.NoError(t, preserved.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, 1, version, "backup must retain the old schema")
	config.Repository, config.GitExecutable = root, ""
	var output bytes.Buffer
	require.NoError(t, cli.Run(t.Context(), []string{"status"}, cli.Streams{Out: &output, Err: &output}, config))
	require.Contains(t, output.String(), "No recorded jobs")
}

func TestMigrationRefusesMissingEmptyForeignAndNewerDatabases(t *testing.T) {
	config := app.Config{Repository: "/missing/repository", GitExecutable: "/missing/git"}
	root := t.TempDir()
	config.DBPath = filepath.Join(root, "missing", "state.db")
	var output bytes.Buffer
	require.ErrorIs(t, cli.Run(t.Context(), []string{"db", "migrate"}, cli.Streams{Out: &output, Err: &output}, config), state.ErrNoDatabase)
	require.NoDirExists(t, filepath.Dir(config.DBPath))
	for name, setup := range map[string]string{"empty": "", "foreign": "CREATE TABLE important(value TEXT)", "newer": "PRAGMA application_id=0x44484e44; PRAGMA user_version=999"} {
		t.Run(name, func(t *testing.T) {
			config.DBPath = filepath.Join(root, name+".db")
			db, err := sql.Open("sqlite", config.DBPath)
			require.NoError(t, err)
			_, err = db.Exec(setup)
			require.NoError(t, err)
			require.NoError(t, db.Close())
			before, err := os.ReadFile(config.DBPath)
			require.NoError(t, err)
			output.Reset()
			err = cli.Run(t.Context(), []string{"db", "migrate"}, cli.Streams{Out: &output, Err: &output}, config)
			require.ErrorIs(t, err, state.ErrSchema)
			require.NotContains(t, err.Error(), "dockhand db migrate")
			if name == "newer" {
				require.Contains(t, err.Error(), "newer Dockhand build")
			}
			after, err := os.ReadFile(config.DBPath)
			require.NoError(t, err)
			require.Equal(t, before, after, fmt.Sprintf("must not mutate %s database", name))
		})
	}
}

func TestGCPrunesSharedIndexCacheWithoutProviderSetup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	out, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput()
	require.NoError(t, err, "%s", out)
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	config := app.Config{Repository: root, DBPath: filepath.Join(root, "state.db"), TclExecutable: "/missing/tcl"}
	config.Tart.Executable = "/missing/tart"
	store, err := sqlite.Open(t.Context(), config.DBPath, sqlite.Options{})
	require.NoError(t, err)
	_, err = store.RegisterRepository(t.Context(), repo.CommonDir)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	cache, err := os.UserCacheDir()
	require.NoError(t, err)
	profile := filepath.Join(cache, "dockhand", "indexes", strings.Repeat("a", 64))
	entry := filepath.Join(profile, "complete", strings.Repeat("b", 40))
	require.NoError(t, os.MkdirAll(entry, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(profile, "index.lock"), nil, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(entry, "PortIndex"), []byte("obsolete cache"), 0600))
	old := time.Now().Add(-30 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(entry, old, old))
	for _, dry := range []bool{true, false} {
		args := []string{"gc", "--json"}
		if dry {
			args = append(args, "--dry-run")
		}
		var output bytes.Buffer
		require.NoError(t, cli.Run(t.Context(), args, cli.Streams{Out: &output, Err: &output}, config))
		var result workflow.RetentionResult
		require.NoError(t, json.Unmarshal(output.Bytes(), &result))
		require.Len(t, result.Items, 1)
		require.Equal(t, "prune-index-cache", result.Items[0].Action)
		require.Equal(t, entry, result.Items[0].Path)
		require.Equal(t, !dry, result.Items[0].Completed)
		if dry {
			require.DirExists(t, entry)
		} else {
			require.NoDirExists(t, entry)
		}
	}
	require.FileExists(t, filepath.Join(profile, "index.lock"))
	require.NoDirExists(t, filepath.Join(root, "artifacts"))
}
