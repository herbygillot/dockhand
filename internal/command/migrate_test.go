package command_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/store/sqlite"
	"github.com/herbygillot/dockhand/internal/term"
)

// A database a release last opened is migrated by a development build
// only at the person's word on a terminal: answered no, nothing changes
// and the refusal stands; answered yes, it migrates, keeping the old form,
// where no command test reached the question (the test analysis's step
// b).
func TestAReleasesDatabaseMigratesAtThePersonsWord(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	// The database where dockhand keeps it: one DOCKHAND_DB names, as a
	// copy, migrates unasked.
	path := filepath.Join(w.Home, ".dockhand", "dockhand.db")
	require.NoError(t, os.Remove(path), "the world's fresh database gives way to the older one")
	require.NoError(t, sqlite.CreateAt(path, 32, "INSERT INTO repositories VALUES('repo_1', '/src/macports-ports/.git', 0);\n"+
		"INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_1','foreground',101,'p','v0.4.0 (5897a828e1c2)',1,1,2);"))

	ask := func(answer string) (string, error) {
		var out, errs bytes.Buffer
		err := cmdtest.RunFor(t, t.Context(), []string{"status"}, term.Streams{In: strings.NewReader(answer), Out: &out, Err: &errs}.AsTerminal())
		return errs.String(), err
	}
	said, err := ask("n\n")
	require.ErrorContains(t, err, "v0.4.0")
	require.Contains(t, said, "? Migrate anyway? A copy using schema 32 will be kept at "+path+".schema-32. [y/N] ")
	require.NoFileExists(t, path+".schema-32", "nothing kept or migrated when the answer is no")

	said, err = ask("y\n")
	require.NoError(t, err, said)
	require.FileExists(t, path+".schema-32", "the release's form is kept")
	_, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err, "migrated, it opens unasked")
}

// A build that isn't a release, as a test's is, migrates the default
// database only at the person's word: asked at a terminal; refused in a
// script, read-only commands and serve included, naming the flag, with
// --yes no answer; --migrate-database migrates it, keeping the copy; a
// database named with --db migrates as before (#281).
func TestABuildThatIsntAReleaseAsksBeforeMigratingTheDefaultDatabase(t *testing.T) {
	t.Parallel()
	older := func(t *testing.T) (cmdtest.World, string) {
		w := cmdtest.NewWorld(t)
		cmdtest.Versioned(t, w)
		path := filepath.Join(w.Home, ".dockhand", "dockhand.db")
		require.NoError(t, os.Remove(path))
		require.NoError(t, sqlite.CreateAt(path, 32, "INSERT INTO repositories VALUES('repo_1', '/src/macports-ports/.git', 0);\n"+
			"INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_1','foreground',101,'p','v0.4.1-0.20261024120000-0123456789ab',1,1,2);"))
		return w, path
	}

	t.Run("asked at a terminal", func(t *testing.T) {
		t.Parallel()
		_, path := older(t)
		ask := func(answer string) (string, error) {
			var out, errs bytes.Buffer
			err := cmdtest.RunFor(t, t.Context(), []string{"status"}, term.Streams{In: strings.NewReader(answer), Out: &out, Err: &errs}.AsTerminal())
			return errs.String(), err
		}
		said, err := ask("n\n")
		require.ErrorContains(t, err, "--migrate-database")
		require.Contains(t, said, "? Migrate it? A copy using schema 32 will be kept at "+path+".schema-32. [y/N] ")
		require.NoFileExists(t, path+".schema-32")
		said, err = ask("y\n")
		require.NoError(t, err, said)
		require.FileExists(t, path+".schema-32")
	})
	t.Run("refused in a script, --yes no answer", func(t *testing.T) {
		t.Parallel()
		_, path := older(t)
		for _, args := range [][]string{{"status"}, {"clean", "--yes"}, {"serve"}} {
			_, _, err := cmdtest.Dockhand(t, args...)
			require.ErrorContains(t, err, "isn't a release, and opening the default database would migrate it from schema 32", args)
			require.ErrorContains(t, err, "run this again with --migrate-database", args)
			require.NoFileExists(t, path+".schema-32", args)
		}
	})
	t.Run("the flag migrates it", func(t *testing.T) {
		t.Parallel()
		_, path := older(t)
		_, _, err := cmdtest.Dockhand(t, "status", "--migrate-database")
		require.NoError(t, err)
		require.FileExists(t, path+".schema-32", "the copy is kept")
	})
	t.Run("another database named migrates as before", func(t *testing.T) {
		t.Parallel()
		w, path := older(t)
		// The default file named with --db is still the default database;
		// another one named is a copy, which migrates on its own.
		named := filepath.Join(w.Home, "copy.db")
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(named, data, 0o600))
		_, _, err = cmdtest.Dockhand(t, "status", "--db", named)
		require.NoError(t, err)
		require.FileExists(t, named+".schema-32")
		_, _, err = cmdtest.Dockhand(t, "status", "--db", path)
		require.ErrorContains(t, err, "--migrate-database", "the default file, named, is still the default database")
	})
}
