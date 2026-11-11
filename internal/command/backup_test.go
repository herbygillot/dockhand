package command_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/store/sqlite"
)

// dockhand backup copies the database to a file, or into a directory
// named by the minute, saying what it copied and what the copy lacks, and
// never replaces a file (#92).
func TestBackupCopiesTheDatabase(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	backups := filepath.Join(w.Home, "Backups")
	require.NoError(t, os.MkdirAll(backups, 0o755))
	out, _, err := cmdtest.Dockhand(t, "backup", "~/Backups")
	require.NoError(t, err)
	copies, err := filepath.Glob(filepath.Join(backups, "dockhand-*.db"))
	require.NoError(t, err)
	require.Len(t, copies, 1)
	require.Regexp(t, `^dockhand-\d{4}-\d{2}-\d{2}-\d{4}\.db$`, filepath.Base(copies[0]))
	flat := cmdtest.Flat(out)
	require.Regexp(t, regexp.MustCompile(`^Copied ~/\.dockhand/dockhand\.db, schema \d+, to ~/Backups/dockhand-[0-9-]+\.db \([0-9.]+ [KMG]?B\)\.\s`), flat)
	require.Contains(t, flat, "Kept archives and check logs aren't in the copy: a check run from it builds again what it can't find, and says once which archives it dropped.")
	info, err := os.Stat(copies[0])
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	_, _, err = cmdtest.Dockhand(t, "backup", copies[0])
	require.ErrorIs(t, err, sqlite.ErrBackupExists)
	require.ErrorContains(t, err, "a backup never replaces a file")
}

// --json carries the source, the copy, the schema, the build that last
// opened the database and when, and the copy's size; a database a newer
// build would migrate is copied as it is, and left as it is.
func TestBackupsJSONAndAnOlderDatabase(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	path := filepath.Join(w.Home, ".dockhand", "dockhand.db")
	require.NoError(t, os.Remove(path))
	require.NoError(t, sqlite.CreateAt(path, 32, "INSERT INTO repositories VALUES('repo_1', '/src/macports-ports/.git', 0);\n"+
		"INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_1','foreground',101,'p','v0.4.0',1762351320000,1,2);"))
	into := filepath.Join(w.Home, "copy.db")
	decoded, err := cmdtest.JSONOf(t, "backup", into)
	require.NoError(t, err)
	require.Equal(t, path, decoded.Result["source"])
	require.Equal(t, into, decoded.Result["copy"])
	require.InDelta(t, 32, decoded.Result["schema"], 0)
	require.Equal(t, "v0.4.0", decoded.Result["build"])
	require.Equal(t, "2025-11-05T14:02:00Z", decoded.Result["opened_at"])
	require.Positive(t, decoded.Result["bytes"])
	left, err := filepath.Glob(path + ".schema-*")
	require.NoError(t, err)
	require.Empty(t, left, "nothing migrated, so no migration copy")
}
