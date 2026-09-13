package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/stretchr/testify/require"
)

func versionOne(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(initialSchema + fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=1;", applicationID))
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO repositories(id,common_dir,created_at) VALUES('preserved','/fixture/repo',1)")
	require.NoError(t, err)
	return path, db
}
func TestProviderMigrationPreservesStateAndSerializesOpeners(t *testing.T) {
	path, db := versionOne(t)
	_, err := Open(t.Context(), path, Options{ReadOnly: true})
	require.ErrorIs(t, err, state.ErrSchema)
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, 1, version)
	start := make(chan struct{})
	done := make(chan error, 6)
	for range 6 {
		go func() {
			<-start
			store, err := Open(context.Background(), path, Options{})
			if err == nil {
				err = store.Close()
			}
			done <- err
		}()
	}
	close(start)
	for range 6 {
		require.NoError(t, <-done)
	}
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, schemaVersion, version)
	store, err := Open(t.Context(), path, Options{ReadOnly: true})
	require.NoError(t, err)
	defer store.Close()
	repo, err := store.FindRepository(t.Context(), "/fixture/repo")
	require.NoError(t, err)
	require.Equal(t, "preserved", string(repo.ID))
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM provider_executions").Scan(&count))
	require.Zero(t, count)
}
func TestProviderMigrationRollsBackOnFailure(t *testing.T) {
	path, db := versionOne(t)
	_, err := db.Exec("CREATE TABLE provider_executions (unrelated TEXT)")
	require.NoError(t, err)
	_, err = Open(t.Context(), path, Options{})
	require.Error(t, err)
	var version, count int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, 1, version)
	require.NoError(t, db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='provider_pools'").Scan(&count))
	require.Zero(t, count, "the first migration statement must also roll back")
	require.NoError(t, db.QueryRow("SELECT count(*) FROM repositories WHERE id='preserved'").Scan(&count))
	require.Equal(t, 1, count)
}
