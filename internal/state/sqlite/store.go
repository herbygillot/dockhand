package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

//go:embed migrations/001.sql
var initialSchema string

//go:embed migrations/002.sql
var providerSchema string

//go:embed migrations/003.sql
var preparationSchema string

//go:embed migrations/004.sql
var releaseSchema string

//go:embed migrations/005.sql
var verificationSchema string

//go:embed migrations/006.sql
var publicationSchema string

//go:embed migrations/007.sql
var imageSchema string

//go:embed migrations/008.sql
var retentionSchema string

//go:embed migrations/009.sql
var phaseSchema string

//go:embed migrations/010.sql
var changeJobsSchema string

//go:embed migrations/011.sql
var imageCapabilitiesSchema string

//go:embed migrations/012.sql
var generationSchema string

//go:embed migrations/013.sql
var sharedRunsSchema string

//go:embed migrations/014.sql
var retrySchema string

//go:embed migrations/015.sql
var contributionSchema string

const schemaVersion = 15
const applicationID = 0x44484e44

type Options struct {
	ReadOnly bool
	// RequireExisting limits writable open to an existing Dockhand database.
	RequireExisting bool
	// AllowOlderSchema is for schema-independent backup and integrity checks.
	// It requires ReadOnly. Do not use older schemas with normal record queries.
	AllowOlderSchema bool
	BusyTimeout      time.Duration
	OperationTimeout time.Duration
}

type Store struct {
	db      *sql.DB
	options Options
	path    string
}

var _ state.Store = (*Store)(nil)

func Open(ctx context.Context, path string, options Options) (*Store, error) {
	if options.AllowOlderSchema && !options.ReadOnly || path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") || options.BusyTimeout < 0 || options.OperationTimeout < 0 {
		return nil, state.ErrInvalid
	}
	if options.BusyTimeout == 0 {
		options.BusyTimeout = 5 * time.Second
	}
	if options.OperationTimeout == 0 {
		options.OperationTimeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, options.OperationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !options.ReadOnly && !options.RequireExisting {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, fmt.Errorf("%w: %w", state.ErrUnavailable, err)
		}
	}
	if resolved, e := filepath.EvalSymlinks(path); e == nil {
		path = resolved
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %w", state.ErrUnavailable, e)
	} else {
		if options.ReadOnly || options.RequireExisting {
			return nil, state.ErrNoDatabase
		}
		parent, e := filepath.EvalSymlinks(filepath.Dir(path))
		if e != nil {
			return nil, e
		}
		path = filepath.Join(parent, filepath.Base(path))
		file, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if e != nil && !errors.Is(e, os.ErrExist) {
			return nil, e
		}
		if e == nil {
			if e = file.Close(); e != nil {
				return nil, e
			}
		}
	}
	mode := "rw"
	if options.ReadOnly {
		mode = "ro"
	}
	params := url.Values{"mode": {mode}, "_pragma": {"foreign_keys(1)", "busy_timeout(" + strconv.FormatInt(max(1, options.BusyTimeout.Milliseconds()), 10) + ")", "synchronous(FULL)"}}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: params.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, storageError(err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	s := &Store{db: db, options: options, path: path}
	if err = s.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Path() string { return s.path }

func (s *Store) initialize(ctx context.Context) error {
	err := s.transaction(ctx, false, "", func(ctx context.Context, t *transaction) error {
		var version, appID, count int
		if err := t.conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return storageError(err)
		}
		if err := t.conn.QueryRowContext(ctx, "PRAGMA application_id").Scan(&appID); err != nil {
			return storageError(err)
		}
		if appID == applicationID && (version == schemaVersion || version >= 1 && version < schemaVersion && (!s.options.ReadOnly || s.options.AllowOlderSchema)) {
			return nil
		}
		if appID != 0 || version != 0 || s.options.ReadOnly || s.options.RequireExisting {
			return schemaMismatch(appID, version)
		}
		if err := t.conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&count); err != nil {
			return storageError(err)
		}
		if count != 0 {
			return state.ErrSchema
		}
		return nil
	})
	if err != nil {
		return err
	}

	if !s.options.ReadOnly {
		deadline := time.Now().Add(s.options.BusyTimeout)
		for {
			var mode string
			err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode)
			if err == nil && mode != "wal" {
				err = s.db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode)
			}
			if err == nil {
				if mode != "wal" {
					return fmt.Errorf("%w: WAL mode required", state.ErrUnavailable)
				}
				break
			}
			var busy interface{ Code() int }
			if !errors.As(err, &busy) || (busy.Code()&255 != 5 && busy.Code()&255 != 6) || time.Now().After(deadline) {
				return fmt.Errorf("WAL setup: %w", storageError(err))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	return s.transaction(ctx, !s.options.ReadOnly, "", func(ctx context.Context, t *transaction) error {
		var version, appID int
		if err := t.conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return storageError(err)
		}
		if err := t.conn.QueryRowContext(ctx, "PRAGMA application_id").Scan(&appID); err != nil {
			return storageError(err)
		}
		if appID == applicationID && (version == schemaVersion || s.options.AllowOlderSchema && version >= 1 && version < schemaVersion) {
			return nil
		}
		if appID == applicationID && version >= 1 && version < schemaVersion && !s.options.ReadOnly {
			return migrateSchema(ctx, t, version)
		}
		if appID != 0 || version != 0 || s.options.ReadOnly || s.options.RequireExisting {
			return schemaMismatch(appID, version)
		}
		var count int
		if err := t.conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&count); err != nil {
			return storageError(err)
		}
		if count != 0 {
			return fmt.Errorf("%w: not a Dockhand database", state.ErrSchema)
		}
		if _, err := t.conn.ExecContext(ctx, initialSchema+fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=1;", applicationID)); err != nil {
			return storageError(err)
		}
		return migrateSchema(ctx, t, 1)
	})
}

func schemaMismatch(appID, version int) error {
	if appID != applicationID {
		return fmt.Errorf("%w: not a recognized Dockhand database", state.ErrSchema)
	}
	if version >= 1 && version < schemaVersion {
		return &state.MigrationRequiredError{Current: version, Required: schemaVersion}
	}
	if version > schemaVersion {
		return fmt.Errorf("%w: database schema %d is newer than this Dockhand supports (%d); use a newer Dockhand build", state.ErrSchema, version, schemaVersion)
	}
	return fmt.Errorf("%w: unrecognized Dockhand schema version %d", state.ErrSchema, version)
}

type schemaMigration struct {
	version int
	schema  string
	apply   func(context.Context, *transaction) error
}

func migrations() []schemaMigration {
	return []schemaMigration{
		{version: 2, schema: providerSchema},
		{version: 3, apply: migratePreparation},
		{version: 4, schema: releaseSchema},
		{version: 5, schema: verificationSchema},
		{version: 6, schema: publicationSchema},
		{version: 7, schema: imageSchema},
		{version: 8, schema: retentionSchema},
		{version: 9, schema: phaseSchema},
		{version: 10, schema: changeJobsSchema},
		{version: 11, schema: imageCapabilitiesSchema},
		{version: 12, schema: generationSchema},
		{version: 13, apply: migrateSharedRuns},
		{version: 14, schema: retrySchema},
		{version: 15, schema: contributionSchema},
	}
}

func migrateSchema(ctx context.Context, t *transaction, current int) error {
	for _, migration := range migrations() {
		if migration.version <= current {
			continue
		}
		if migration.version != current+1 {
			return fmt.Errorf("%w: missing migration after schema %d", state.ErrSchema, current)
		}
		var err error
		if migration.apply != nil {
			err = migration.apply(ctx, t)
		} else {
			_, err = t.conn.ExecContext(ctx, migration.schema)
			err = storageError(err)
		}
		if err != nil {
			return err
		}
		if _, err = t.conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d;", migration.version)); err != nil {
			return storageError(err)
		}
		current = migration.version
	}
	if current != schemaVersion {
		return fmt.Errorf("%w: incomplete migration at schema %d", state.ErrSchema, current)
	}
	return nil
}

func (s *Store) FindRepository(ctx context.Context, path string) (record.Repository, error) {
	var result record.Repository
	err := s.transaction(ctx, false, "", func(ctx context.Context, t *transaction) error {
		var created int64
		err := t.conn.QueryRowContext(ctx, "SELECT id,common_dir,created_at FROM repositories WHERE common_dir=?", path).Scan(&result.ID, &result.CommonDir, &created)
		result.CreatedAt = fromTime(created)
		return storageError(err)
	})
	return result, err
}
func (s *Store) RegisterRepository(ctx context.Context, path string) (record.Repository, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return record.Repository{}, state.ErrInvalid
	}
	var result record.Repository
	err := s.transaction(ctx, true, "", func(ctx context.Context, t *transaction) error {
		_, err := t.conn.ExecContext(ctx, "INSERT INTO repositories(id,common_dir,created_at) VALUES(?,?,?) ON CONFLICT(common_dir) DO NOTHING", "repo_"+rand.Text(), path, time.Now().UTC().UnixMilli())
		if err != nil {
			return storageError(err)
		}
		var created int64
		err = t.conn.QueryRowContext(ctx, "SELECT id,common_dir,created_at FROM repositories WHERE common_dir=?", path).Scan(&result.ID, &result.CommonDir, &created)
		result.CreatedAt = fromTime(created)
		return storageError(err)
	})
	return result, err
}

func migratePreparation(ctx context.Context, t *transaction) error {
	return migrateRebuiltTables(ctx, t, preparationSchema)
}

func migrateSharedRuns(ctx context.Context, t *transaction) error {
	return migrateRebuiltTables(ctx, t, sharedRunsSchema)
}

func migrateRebuiltTables(ctx context.Context, t *transaction, schema string) error {
	if _, err := t.conn.ExecContext(ctx, schema); err != nil {
		return storageError(err)
	}
	rows, err := t.conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return storageError(err)
	}
	invalid := rows.Next()
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return storageError(err)
	}
	if invalid {
		return fmt.Errorf("%w: migration left invalid references", state.ErrSchema)
	}
	_, err = t.conn.ExecContext(ctx, "PRAGMA defer_foreign_keys=OFF;")
	return storageError(err)
}
