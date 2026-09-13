package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
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

func versionTwoWithWork(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, db := versionOne(t)
	_, err := db.Exec(providerSchema + `PRAGMA user_version=2;
PRAGMA foreign_keys=ON;
BEGIN;
INSERT INTO sources VALUES('preserved','source','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',NULL);
INSERT INTO changes VALUES('change','preserved','candidate',NULL,'open','[]',1);
INSERT INTO revisions VALUES('revision','preserved','change','source',NULL,1);
UPDATE changes SET current_revision='revision';
INSERT INTO requests VALUES('request','preserved','job',X'7b7d',1,NULL);
INSERT INTO jobs VALUES('job','preserved','request','change','change','revision',NULL,'source','verify','verification-complete','required','{}','active',1,NULL,2,NULL,'running',3);
INSERT INTO plans VALUES('preserved','job','revision','[]');
INSERT INTO attempts VALUES('attempt','preserved','job','target','revision','source','{}','running','driver',2,90,3,90,NULL,0,'',1);
INSERT INTO submissions VALUES('submission','preserved','attempt',1,'tart','run',1,2,NULL);
INSERT INTO attempt_evidence VALUES('preserved','attempt','{"Verdict":"unknown"}');
INSERT INTO resources VALUES('resource','preserved','attempt','submission','tart','vm','active',NULL,0,NULL,NULL,NULL,NULL,NULL,'');
INSERT INTO provider_pools VALUES('pool','scope','/fixture/pool',2);
INSERT INTO provider_executions VALUES('submission','pool','preserved','attempt','vm',X'7b7d','admitted',1,NULL,1);
COMMIT;`)
	require.NoError(t, err)
	return path, db
}

func migrationRows(t *testing.T, db *sql.DB, table string, columns []string) ([]string, [][]any) {
	t.Helper()
	selection := "*"
	if len(columns) > 0 {
		selection = strings.Join(columns, ",")
	}
	rows, err := db.Query("SELECT " + selection + " FROM " + table + " ORDER BY rowid")
	require.NoError(t, err)
	defer rows.Close()
	columns, err = rows.Columns()
	require.NoError(t, err)
	result := [][]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		args := make([]any, len(columns))
		for i := range values {
			args[i] = &values[i]
		}
		require.NoError(t, rows.Scan(args...))
		result = append(result, values)
	}
	require.NoError(t, rows.Err())
	return columns, result
}

func TestPreparationMigrationPreservesPopulatedExecutionGraph(t *testing.T) {
	path, db := versionTwoWithWork(t)
	tables := []string{"repositories", "sources", "changes", "revisions", "requests", "jobs", "plans", "attempts", "submissions", "attempt_evidence", "resources", "provider_pools", "provider_executions"}
	columns := map[string][]string{}
	before := map[string][][]any{}
	for _, table := range tables {
		columns[table], before[table] = migrationRows(t, db, table, nil)
	}
	store, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	defer store.Close()
	for _, table := range tables {
		_, after := migrationRows(t, db, table, columns[table])
		require.Equal(t, before[table], after, table)
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	require.NoError(t, err)
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	_, err = db.Exec("UPDATE plans SET revision_id=NULL; UPDATE attempts SET revision_id=NULL;")
	require.NoError(t, err, "standalone verification can omit a contribution revision")
	_, err = db.Exec("DELETE FROM attempts WHERE id='attempt'")
	require.Error(t, err, "migration must preserve child foreign keys")
}

func TestPreparationMigrationRollsBackAddedColumnsAndTableRebuild(t *testing.T) {
	path, db := versionTwoWithWork(t)
	_, err := db.Exec("CREATE TABLE attempts_new(unrelated TEXT)")
	require.NoError(t, err)
	_, err = Open(t.Context(), path, Options{})
	require.Error(t, err)
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, 2, version)
	columns, rows := migrationRows(t, db, "jobs", nil)
	require.NotContains(t, columns, "prepared")
	require.Len(t, rows, 1)
	_, err = db.Exec("UPDATE plans SET revision_id=NULL")
	require.Error(t, err, "the earlier plans rebuild must also roll back")
}
