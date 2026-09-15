package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
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
	_, err = Open(t.Context(), path, Options{RequireExisting: true})
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

func versionEightWithWork(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, db := versionTwoWithWork(t)
	_, err := db.Exec("BEGIN;" + preparationSchema + "PRAGMA defer_foreign_keys=OFF;" + releaseSchema + verificationSchema + publicationSchema + imageSchema + retentionSchema + "COMMIT;")
	require.NoError(t, err)
	return path, db
}

func versionTenWithWork(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, db := versionEightWithWork(t)
	_, err := db.Exec(phaseSchema + changeJobsSchema)
	require.NoError(t, err)
	return path, db
}

func TestMigrationsAreContiguous(t *testing.T) {
	migrations := migrations()
	require.Len(t, migrations, schemaVersion-1)
	for i, migration := range migrations {
		require.Equal(t, i+2, migration.version)
		require.NotEqual(t, migration.schema == "", migration.apply == nil, "migration %d must have exactly one implementation", migration.version)
	}
}

func TestChangeJobsMigrationSupportsIndexedSelection(t *testing.T) {
	path, db := versionEightWithWork(t)
	_, err := db.Exec(phaseSchema)
	require.NoError(t, err)
	store, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	defer store.Close()
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, schemaVersion, version)
	rows, err := db.Query("EXPLAIN QUERY PLAN SELECT j.id FROM jobs j INDEXED BY jobs_change WHERE j.repository_id=? AND j.id>? AND j.state IN ('queued','active') AND j.change_id=? ORDER BY j.id LIMIT ?", "preserved", "", "change", 256)
	require.NoError(t, err)
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plans = append(plans, detail)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, strings.Join(plans, "\n"), "jobs_change")
}

func TestImageCapabilitiesMigrationPreservesProviderExecutions(t *testing.T) {
	path, db := versionTenWithWork(t)
	columns, before := migrationRows(t, db, "provider_executions", nil)
	store, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	defer store.Close()
	_, after := migrationRows(t, db, "provider_executions", columns)
	require.Equal(t, before, after)

	value := state.ImageCapabilities{
		Provider: "tart", EnvironmentDigest: "sha256:image", CapabilityDigest: "sha256:capabilities",
		Capabilities: record.EnvironmentCapabilities{
			Platform:       record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"},
			MacPortsPrefix: "/opt/local", MacPortsVersion: "2.12.6", DeveloperTools: record.DeveloperToolsCommandLine,
		},
		ObservedAt: time.UnixMilli(123).UTC(),
	}
	require.NoError(t, store.PutImageCapabilities(t.Context(), value))
	loaded, err := store.ImageCapabilities(t.Context(), value.Provider, value.EnvironmentDigest)
	require.NoError(t, err)
	require.Equal(t, value, loaded)
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

func TestReleaseMigrationPreservesPreparedWork(t *testing.T) {
	path, db := versionTwoWithWork(t)
	_, err := db.Exec("BEGIN;" + preparationSchema + `PRAGMA defer_foreign_keys=OFF; PRAGMA user_version=3;
 UPDATE jobs SET prepared='{"Branch":"dockhand/revbump/fixture","Source":{"Commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","Tree":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"IntegrationStarted":true}'; COMMIT;`)
	require.NoError(t, err)
	var before string
	require.NoError(t, db.QueryRow("SELECT prepared FROM jobs WHERE id='job'").Scan(&before))
	store, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	defer store.Close()
	var after string
	var release sql.NullString
	require.NoError(t, db.QueryRow("SELECT prepared,resolved_release FROM jobs WHERE id='job'").Scan(&after, &release))
	require.Equal(t, before, after)
	require.False(t, release.Valid)
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, schemaVersion, version)
}

func TestVerificationMigrationPreservesHistoryAndBuildsLookupIndexes(t *testing.T) {
	path, db := versionTwoWithWork(t)
	_, err := db.Exec("BEGIN;" + preparationSchema + "PRAGMA defer_foreign_keys=OFF;" + releaseSchema + "COMMIT;")
	require.NoError(t, err)
	tables := []string{"jobs", "sources", "attempts", "attempt_evidence", "submissions", "resources"}
	columns := map[string][]string{}
	before := map[string][][]any{}
	for _, table := range tables {
		columns[table], before[table] = migrationRows(t, db, table, nil)
	}
	_, err = Open(t.Context(), path, Options{ReadOnly: true})
	require.ErrorIs(t, err, state.ErrSchema)
	store, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	defer store.Close()
	for _, table := range tables {
		_, after := migrationRows(t, db, table, columns[table])
		require.Equal(t, before[table], after, table)
	}
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, schemaVersion, version)
	for _, query := range []struct {
		sql     string
		args    []any
		indexes []string
	}{
		{verificationTreeSQL, []any{"preserved", strings.Repeat("b", 40), "fixture", "devel/fixture/Portfile", 32}, []string{"sources_reuse_tree", "attempts_reuse_source"}},
		{verificationTargetSQL, []any{"preserved", "fixture", "devel/fixture/Portfile", 1}, []string{"attempts_reuse_target"}},
	} {
		rows, err := db.Query("EXPLAIN QUERY PLAN "+query.sql, query.args...)
		require.NoError(t, err)
		var plans []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
			plans = append(plans, detail)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		plan := strings.Join(plans, "\n")
		for _, index := range query.indexes {
			require.Contains(t, plan, index)
		}
		require.NotContains(t, plan, "SCAN a")
		require.NotContains(t, plan, "SCAN s")
	}
}

func TestVerificationMigrationRollsBackWithoutDisturbingSchemaFour(t *testing.T) {
	path, db := versionTwoWithWork(t)
	_, err := db.Exec("BEGIN;" + preparationSchema + "PRAGMA defer_foreign_keys=OFF;" + releaseSchema + "COMMIT; CREATE TABLE sources_reuse_tree(unrelated TEXT);")
	require.NoError(t, err)
	_, err = Open(t.Context(), path, Options{})
	require.Error(t, err)
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, 4, version)
	columns, rows := migrationRows(t, db, "jobs", nil)
	require.NotContains(t, columns, "reused_attempt")
	require.Len(t, rows, 1)
}

func TestPublicationMigrationPreservesSchemaFiveHistory(t *testing.T) {
	path, db := versionTwoWithWork(t)
	_, err := db.Exec("BEGIN;" + preparationSchema + "PRAGMA defer_foreign_keys=OFF;" + releaseSchema + verificationSchema + "COMMIT;")
	require.NoError(t, err)
	tables := []string{"changes", "revisions", "jobs", "sources", "attempts", "attempt_evidence", "provider_executions"}
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
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, schemaVersion, version)
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM publications").Scan(&count))
	require.Zero(t, count)
}

func TestPublicationMigrationFailureRollsBackAdditions(t *testing.T) {
	path, db := versionTwoWithWork(t)
	_, err := db.Exec("BEGIN;" + preparationSchema + "PRAGMA defer_foreign_keys=OFF;" + releaseSchema + verificationSchema + "COMMIT; CREATE TABLE publications(unrelated TEXT);")
	require.NoError(t, err)
	_, err = Open(t.Context(), path, Options{})
	require.Error(t, err)
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, 5, version)
	columns, _ := migrationRows(t, db, "changes", nil)
	require.NotContains(t, columns, "pull_request_id")
}

func TestImageCacheMigrationPreservesSchemaSixAndRollsBackOnConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			path, db := versionTwoWithWork(t)
			_, err := db.Exec("BEGIN;" + preparationSchema + "PRAGMA defer_foreign_keys=OFF;" + releaseSchema + verificationSchema + publicationSchema + "COMMIT;")
			require.NoError(t, err)
			_, before := migrationRows(t, db, "provider_executions", nil)
			if conflict {
				_, err = db.Exec("CREATE TABLE image_digests(unrelated TEXT)")
				require.NoError(t, err)
			}
			store, err := Open(t.Context(), path, Options{})
			if conflict {
				require.Error(t, err)
				var version int
				require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
				require.Equal(t, 6, version)
			} else {
				require.NoError(t, err)
				defer store.Close()
				value := state.ImageDigest{Provider: "tart", Path: "/images/base", Stamp: "observed", Digest: "digest"}
				require.NoError(t, store.PutImageDigest(t.Context(), value))
				loaded, err := store.ImageDigest(t.Context(), "tart", "/images/base")
				require.NoError(t, err)
				require.Equal(t, value, loaded)
				_, err = store.ImageDigest(t.Context(), "another-provider", "/images/base")
				require.ErrorIs(t, err, state.ErrNotFound)
			}
			_, after := migrationRows(t, db, "provider_executions", nil)
			require.Equal(t, before, after)
		})
	}
}

func TestRetentionMigrationPreservesReleasedResources(t *testing.T) {
	path, db := versionTwoWithWork(t)
	_, err := db.Exec("BEGIN;" + preparationSchema + "PRAGMA defer_foreign_keys=OFF;" + releaseSchema + verificationSchema + publicationSchema + imageSchema + "COMMIT;")
	require.NoError(t, err)
	_, err = db.Exec("UPDATE resources SET state='released',released_at=123,next_action_at=NULL")
	require.NoError(t, err)
	columns, before := migrationRows(t, db, "resources", nil)
	store, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	defer store.Close()
	_, after := migrationRows(t, db, "resources", columns)
	require.Equal(t, before, after)
	var pruned sql.NullInt64
	require.NoError(t, db.QueryRow("SELECT artifacts_pruned_at FROM resources").Scan(&pruned))
	require.False(t, pruned.Valid)
	require.NoError(t, store.Check(t.Context()))
}

func TestPhaseMigrationBackfillsWorkflowOwnership(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup string
		want  string
	}{
		{name: "standalone verification", want: "verification"},
		{name: "standalone publication", setup: "UPDATE jobs SET action='publish';", want: "publication"},
		{name: "branch ready result", setup: "UPDATE jobs SET action='bump-revision',destination='branch-ready',result_revision='revision';", want: "preparation"},
		{name: "preparation pending", setup: "UPDATE jobs SET action='bump-revision',destination='published',result_revision=NULL;", want: "preparation"},
		{name: "verification pending", setup: "UPDATE jobs SET action='bump-revision',destination='published',result_revision='revision';", want: "verification"},
		{name: "verification passed", setup: `UPDATE jobs SET action='bump-revision',destination='published',result_revision='revision'; UPDATE attempts SET state='finished'; UPDATE attempt_evidence SET evidence='{"Verdict":"passed"}';`, want: "publication"},
		{name: "verification reused", setup: "UPDATE jobs SET action='bump-revision',destination='published',result_revision='revision',reused_attempt='attempt';", want: "publication"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, db := versionEightWithWork(t)
			if test.setup != "" {
				_, err := db.Exec(test.setup)
				require.NoError(t, err)
			}
			store, err := Open(t.Context(), path, Options{})
			require.NoError(t, err)
			defer store.Close()
			var phase string
			require.NoError(t, db.QueryRow("SELECT phase FROM jobs WHERE id='job'").Scan(&phase))
			require.Equal(t, test.want, phase)
			var version int
			require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
			require.Equal(t, schemaVersion, version)
		})
	}
}

func TestPhaseMigrationFailureLeavesSchemaEightVersion(t *testing.T) {
	path, db := versionEightWithWork(t)
	_, err := db.Exec("ALTER TABLE jobs ADD COLUMN phase TEXT")
	require.NoError(t, err)
	_, err = Open(t.Context(), path, Options{})
	require.Error(t, err)
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, 8, version)
}

func TestMaintenanceCanBackUpOlderSchemaWithoutMigrating(t *testing.T) {
	path, db := versionOne(t)
	store, err := Open(t.Context(), path, Options{ReadOnly: true, AllowOlderSchema: true})
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.Check(t.Context()))
	result, err := store.Backup(t.Context(), filepath.Join(t.TempDir(), "old.db"))
	require.NoError(t, err)
	var version int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, 1, version)
	restored, err := sql.Open("sqlite", result.Path)
	require.NoError(t, err)
	defer restored.Close()
	require.NoError(t, restored.QueryRow("PRAGMA user_version").Scan(&version))
	require.Equal(t, 1, version)
	_, err = Open(t.Context(), path, Options{ReadOnly: true})
	require.ErrorIs(t, err, state.ErrSchema)
	_, err = Open(t.Context(), path, Options{AllowOlderSchema: true})
	require.ErrorIs(t, err, state.ErrInvalid)
}

func TestReadOnlySchemaErrorDistinguishesSupportedUpgrade(t *testing.T) {
	path, db := versionOne(t)
	for _, scenario := range []struct {
		appID, version int
		upgrade        bool
	}{
		{applicationID, 1, true}, {applicationID, schemaVersion + 1, false}, {1234, 1, false}, {applicationID, 0, false},
	} {
		_, err := db.Exec(fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", scenario.appID, scenario.version))
		require.NoError(t, err)
		_, err = Open(t.Context(), path, Options{ReadOnly: true})
		require.ErrorIs(t, err, state.ErrSchema)
		var migration *state.MigrationRequiredError
		if scenario.upgrade {
			require.ErrorAs(t, err, &migration)
			require.Equal(t, 1, migration.Current)
			require.Equal(t, schemaVersion, migration.Required)
		} else {
			require.False(t, errors.As(err, &migration))
		}
	}
}

func TestSharedRunMigrationPreservesReferencesAndAttemptExclusivity(t *testing.T) {
	path, db := versionTenWithWork(t)
	_, err := db.Exec(imageCapabilitiesSchema + generationSchema + "PRAGMA user_version=12;")
	require.NoError(t, err)
	cols, before := migrationRows(t, db, "submissions", nil)
	resourceCols, resources := migrationRows(t, db, "resources", nil)
	store, err := Open(t.Context(), path, Options{})
	require.NoError(t, err)
	defer store.Close()
	_, after := migrationRows(t, db, "submissions", cols)
	require.Equal(t, before, after)
	_, afterResources := migrationRows(t, db, "resources", resourceCols)
	require.Equal(t, resources, afterResources)
	attemptCols, _ := migrationRows(t, db, "attempts", nil)
	selection := append([]string(nil), attemptCols...)
	for i, column := range selection {
		if column == "id" {
			selection[i] = "'observer'"
		}
	}
	_, err = db.Exec("INSERT INTO attempts (" + strings.Join(attemptCols, ",") + ") SELECT " + strings.Join(selection, ",") + " FROM attempts WHERE id='attempt'")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO submissions VALUES('observer-submission','preserved','observer',1,'tart','run',1,2,NULL)")
	require.NoError(t, err, "a provider run may have independent observers")
	_, err = db.Exec("INSERT INTO submissions VALUES('duplicate','preserved','observer',2,'tart','different-run',1,2,NULL)")
	require.Error(t, err, "each attempt still has at most one live submission")
	rows, err := db.Query("PRAGMA foreign_key_check")
	require.NoError(t, err)
	defer rows.Close()
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
}
