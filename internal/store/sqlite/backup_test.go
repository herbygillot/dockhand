package sqlite

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/store"
)

// A backup is the database whole, opened with the same rows, compacted to
// one file readable only by its owner; its directory is made; and it's
// made while another process holds a write transaction, a reader not
// blocking a writer in WAL mode (#92).
func TestABackupIsTheDatabaseWholeWhileAWriterWrites(t *testing.T) {
	t.Parallel()
	f := open(t)
	f.seed(t)
	conn, err := f.db(t).Conn(t.Context())
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.ExecContext(t.Context(), "BEGIN IMMEDIATE")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), "UPDATE branches SET title='uncommitted'")
	require.NoError(t, err)

	into := filepath.Join(t.TempDir(), "made", "dockhand-backup.db")
	made, err := Backup(t.Context(), f.path, into, Options{})
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), "ROLLBACK")
	require.NoError(t, err)
	require.Equal(t, store.Backup{Source: f.path, Copy: into, Schema: schemaVersion, Bytes: made.Bytes}, made, "no session recorded, so no build")
	require.Positive(t, made.Bytes)
	info, err := os.Stat(into)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.NoFileExists(t, into+"-wal", "a plain file with no WAL")

	copied, err := Open(t.Context(), into, Options{})
	require.NoError(t, err)
	defer copied.Close()
	require.NoError(t, copied.View(t.Context(), f.repo, func(r store.Reader) error {
		branches, err := r.Branches(store.BranchFilter{})
		require.NoError(t, err)
		require.Len(t, branches, 1)
		require.Empty(t, branches[0].Title, "the committed rows, not the writer's uncommitted change")
		return nil
	}))
	left, err := filepath.Glob(into + partialCopy + "*")
	require.NoError(t, err)
	require.Empty(t, left)
}

// A backup names the build that last opened the database, and backs up one
// of an older schema without migrating it, or one of a newer schema this
// build can't open.
func TestABackupReadsTheDatabaseWithoutMigratingIt(t *testing.T) {
	t.Parallel()
	opened := time.Date(2026, 11, 5, 14, 2, 0, 0, time.UTC)
	older := schemaAt(t, schemaVersion-1, schema23Records+fmt.Sprintf(`
INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_old','foreground',101,'p','v0.7.0',1,1,1);
INSERT INTO sessions(repository_id, id, kind, pid, process_start, version, started_at, heartbeat_at, ended_at) VALUES('repo_1','ses_last','foreground',102,'p','v0.8.0',%[1]d,%[1]d,%[1]d);
`, opened.UnixMilli()))
	made, err := Backup(t.Context(), older, older+".backup", Options{})
	require.NoError(t, err)
	require.Equal(t, schemaVersion-1, made.Schema)
	require.Equal(t, "v0.8.0", made.Build)
	require.True(t, opened.Equal(made.OpenedAt), "when the newest session started")
	require.Equal(t, schemaVersion-1, versionOf(t, older), "the database isn't migrated")
	require.Equal(t, schemaVersion-1, versionOf(t, older+".backup"))
	left, err := filepath.Glob(older + ".schema-*")
	require.NoError(t, err)
	require.Empty(t, left, "no migration copy either")

	newer := filepath.Join(t.TempDir(), "newer.db")
	db, err := sql.Open("sqlite", newer)
	require.NoError(t, err)
	_, err = db.Exec(fmt.Sprintf("CREATE TABLE sessions(version TEXT, started_at INTEGER); PRAGMA application_id=%d; PRAGMA user_version=%d;", applicationID, schemaVersion+1))
	require.NoError(t, err)
	require.NoError(t, db.Close())
	made, err = Backup(t.Context(), newer, newer+".backup", Options{})
	require.NoError(t, err)
	require.Equal(t, schemaVersion+1, made.Schema)
}

// versionOf is a database file's schema version.
func versionOf(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	defer db.Close()
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	return version
}

// A backup refuses what isn't a dockhand v3 database, by name, a file
// already where the copy goes, and a database that isn't there; and a
// copy cut short leaves nothing.
func TestABackupRefusesWhatItShouldntCopy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	v2 := filepath.Join(dir, "v2.db")
	db, err := sql.Open("sqlite", v2)
	require.NoError(t, err)
	_, err = db.Exec(fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=24;", v2ApplicationID))
	require.NoError(t, err)
	require.NoError(t, db.Close())
	_, err = Backup(t.Context(), v2, filepath.Join(dir, "v2.backup"), Options{})
	require.ErrorIs(t, err, store.ErrSchema)
	require.ErrorContains(t, err, "dockhand v2 database")

	text := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(text, []byte("not a database, but long enough to have a header's worth of bytes in it, and then some more"), 0o600))
	_, err = Backup(t.Context(), text, filepath.Join(dir, "notes.backup"), Options{})
	require.ErrorIs(t, err, store.ErrSchema)
	require.ErrorContains(t, err, "notes.txt is not a dockhand database")

	_, err = Backup(t.Context(), filepath.Join(dir, "missing.db"), filepath.Join(dir, "missing.backup"), Options{})
	require.ErrorIs(t, err, store.ErrUnavailable)
	require.NoFileExists(t, filepath.Join(dir, "missing.db"), "nothing is made where there was no database")

	f := open(t)
	there := filepath.Join(dir, "there.db")
	require.NoError(t, os.WriteFile(there, []byte("keep me"), 0o600))
	_, err = Backup(t.Context(), f.path, there, Options{})
	require.ErrorIs(t, err, ErrBackupExists)
	kept, err := os.ReadFile(there)
	require.NoError(t, err)
	require.Equal(t, "keep me", string(kept))

	for _, refused := range []string{"v2.backup", "notes.backup", "missing.backup"} {
		require.NoFileExists(t, filepath.Join(dir, refused))
	}
	left, err := filepath.Glob(filepath.Join(dir, "*"+partialCopy+"*"))
	require.NoError(t, err)
	require.Empty(t, left)
}

// A copy cut short, by a page it can't read as a timeout or the process's
// end would stop it, leaves nothing at the name or beside it.
func TestABackupCutShortLeavesNothing(t *testing.T) {
	t.Parallel()
	path := schemaAt(t, schemaVersion-1, schema23Records+
		"CREATE TABLE filler(x); WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<200) INSERT INTO filler SELECT randomblob(1000) FROM n;")
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	info, err := file.Stat()
	require.NoError(t, err)
	_, err = file.WriteAt(bytes.Repeat([]byte{0xff}, 4096), info.Size()-4096)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	into := filepath.Join(t.TempDir(), "backup.db")
	_, err = Backup(t.Context(), path, into, Options{})
	require.ErrorIs(t, err, store.ErrUnavailable)
	require.ErrorContains(t, err, "nothing was copied")
	require.NoFileExists(t, into)
	left, err := filepath.Glob(into + partialCopy + "*")
	require.NoError(t, err)
	require.Empty(t, left)
}
