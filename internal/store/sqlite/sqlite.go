// Package sqlite implements the store contract on one SQLite database,
// dockhand v3's ~/.dockhand/dockhand.db.
//
// The database is opened in WAL mode with foreign keys on and full sync,
// through to the disk on macOS, and its planner's statistics kept. A
// write transaction begins IMMEDIATE, so writers queue on SQLite's lock
// instead of failing midway, and a commit whose outcome is unknown says so
// with store.ErrUncertain. The schema carries the rules it can check by
// itself: valid states, unique run numbers and snapshot numbers, final
// checkpoints, and lease generations that never decrease.
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	sqlitedriver "modernc.org/sqlite"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// schemas are the schema's versions in order; schemas[n-1] takes a
// database from version n-1 to n. A new database gets them all.
//
//go:embed schema/*.sql
var schemaFiles embed.FS

var schemas = func() []string {
	var all []string
	for n := 1; ; n++ {
		data, err := schemaFiles.ReadFile(fmt.Sprintf("schema/%03d.sql", n))
		if err != nil {
			return all
		}
		all = append(all, string(data))
	}
}()

// schemaVersion is the newest schema this build writes.
var schemaVersion = len(schemas)

// applicationID marks a dockhand v3 database ("DHN3"). v2's databases carry
// 0x44484e44 ("DHND") and are refused by name.
const (
	applicationID   = 0x44484e33
	v2ApplicationID = 0x44484e44
)

// journalSizeLimit is the most WAL file, in bytes, a checkpoint leaves.
const journalSizeLimit = 64 << 20

// Options tunes how the database is opened.
type Options struct {
	BusyTimeout      time.Duration
	OperationTimeout time.Duration
}

// Store is a dockhand v3 database.
type Store struct {
	db      *sql.DB
	options Options
	path    string
	// migration is the migration Open made, if it made one.
	migration *Migration
}

// Migration is a schema migration Open made, and where the database's
// form before it is kept: a build older than the migration can't open the
// database after it, and can open the copy.
type Migration struct {
	From, To int
	Copy     string
}

// Migration is the migration Open made; nil where it made none.
func (s *Store) Migration() *Migration { return s.migration }

// keptCopies is how long the copies migrations keep are kept, beyond the
// newest one.
const keptCopies = 30 * 24 * time.Hour

var _ store.Store = (*Store)(nil)

// Open opens the database at path, creating it and its directory when
// absent, and refuses a file that is not a dockhand v3 database.
func Open(ctx context.Context, path string, options Options) (*Store, error) {
	if path == "" || path == ":memory:" || options.BusyTimeout < 0 || options.OperationTimeout < 0 {
		return nil, fmt.Errorf("%w: database path %q", store.ErrUnavailable, path)
	}
	if options.BusyTimeout == 0 {
		options.BusyTimeout = 5 * time.Second
	}
	if options.OperationTimeout == 0 {
		options.OperationTimeout = 30 * time.Second
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("%w: %w", store.ErrUnavailable, err)
	}
	// fullfsync and checkpoint_fullfsync make a sync reach the disk on
	// macOS, whose fsync leaves writes in the drive's cache. A checkpoint
	// is recorded as prepared before its Git change, and must outlast a
	// power loss that the change outlasts (docs/reviews/2026-09-28-sql-
	// review.md, finding 7). journal_size_limit returns the WAL file's
	// space once a large transaction is checkpointed.
	params := url.Values{"_pragma": {
		"foreign_keys(1)",
		"busy_timeout(" + strconv.FormatInt(max(1, options.BusyTimeout.Milliseconds()), 10) + ")",
		"synchronous(FULL)",
		"fullfsync(1)",
		"checkpoint_fullfsync(1)",
		"journal_size_limit(" + strconv.Itoa(journalSizeLimit) + ")",
	}}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: params.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, storageError(err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	s := &Store{db: db, options: options, path: path}
	if err := s.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Path is the database's absolute path.
func (s *Store) Path() string { return s.path }

func (s *Store) initialize(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.options.OperationTimeout)
	defer cancel()
	var mode string
	deadline := time.Now().Add(s.options.BusyTimeout)
	for {
		err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode)
		if err == nil {
			break
		}
		if !busy(err) || time.Now().After(deadline) {
			return fmt.Errorf("WAL setup: %w", storageError(err))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	if mode != "wal" {
		return fmt.Errorf("%w: WAL mode required, got %q", store.ErrUnavailable, mode)
	}
	if err := s.keepBeforeMigrating(ctx); err != nil {
		return err
	}
	if err := s.prepareSchema(ctx); err != nil {
		return err
	}
	// The planner chooses among indexes by statistics kept in the database;
	// without them it can walk a whole table to spare itself a sort, as it
	// did for the queued runs serve polls for. Those are a few among many,
	// which only sampled values (sqlite_stat4) show: an average says each
	// state holds half the runs. analysis_limit would skip the samples, so
	// it stays unset. optimize analyzes a table again only once it has
	// grown many times over, so most opens do nothing.
	if _, err := s.db.ExecContext(ctx, "PRAGMA optimize=0x10002"); err != nil {
		return fmt.Errorf("planner statistics: %w", storageError(err))
	}
	return nil
}

// keepBeforeMigrating copies a database this build would migrate, as it is,
// beside itself, dockhand.db.schema-24, before the migration, and prunes
// the copies earlier migrations kept once they're old. VACUUM INTO makes a
// consistent copy, as SQLite documents, and runs outside a transaction, so
// it comes before prepareSchema's; another process may migrate between the
// two, which leaves a copy of the older form, as intended. Where the copy
// can't be made, nothing is migrated.
func (s *Store) keepBeforeMigrating(ctx context.Context) error {
	var version, app int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return storageError(err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
		return storageError(err)
	}
	if app != applicationID || version >= schemaVersion {
		return nil
	}
	copy := fmt.Sprintf("%s.schema-%d", s.path, version)
	if _, err := os.Stat(copy); errors.Is(err, os.ErrNotExist) {
		if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", copy); err != nil {
			return fmt.Errorf("%w: keeping a copy of %s before migrating it from schema %d to %d: %w; nothing was migrated", store.ErrUnavailable, s.path, version, schemaVersion, storageError(err))
		}
		if err := os.Chmod(copy, 0o600); err != nil {
			return err
		}
	}
	s.migration = &Migration{From: version, To: schemaVersion, Copy: copy}
	earlier, _ := filepath.Glob(s.path + ".schema-*")
	for _, path := range earlier {
		if info, err := os.Stat(path); err == nil && path != copy && time.Since(info.ModTime()) > keptCopies {
			_ = os.Remove(path)
		}
	}
	return nil
}

// prepareSchema checks that the database is dockhand v3's, and brings its
// schema up to this build's, or makes it for an empty file.
func (s *Store) prepareSchema(ctx context.Context) error {
	return s.transaction(ctx, true, "", func(t *tx) error {
		var version, app, tables int
		if err := t.conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return storageError(err)
		}
		if err := t.conn.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
			return storageError(err)
		}
		switch {
		case app == applicationID && version == schemaVersion:
			return nil
		case app == applicationID && version > 0 && version < schemaVersion:
			return t.migrate(ctx, version)
		case app == applicationID && version > schemaVersion:
			return fmt.Errorf("%w: %s has schema %d, newer than this dockhand supports (%d); use a newer build", store.ErrSchema, s.path, version, schemaVersion)
		case app == v2ApplicationID:
			return fmt.Errorf("%w: %s is a dockhand v2 database; v3 keeps its records in its own file and never reads v2's", store.ErrSchema, s.path)
		case app != 0 || version != 0:
			return fmt.Errorf("%w: %s is not a dockhand database", store.ErrSchema, s.path)
		}
		if err := t.conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
			return storageError(err)
		}
		if tables != 0 {
			return fmt.Errorf("%w: %s holds tables but is not a dockhand database", store.ErrSchema, s.path)
		}
		if _, err := t.conn.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id=%d;", applicationID)); err != nil {
			return storageError(err)
		}
		return t.migrate(ctx, 0)
	})
}

// migrate brings the schema from version up to schemaVersion, within the
// caller's transaction.
func (t *tx) migrate(ctx context.Context, version int) error {
	for n := version; n < schemaVersion; n++ {
		if _, err := t.conn.ExecContext(ctx, schemas[n]+fmt.Sprintf("PRAGMA user_version=%d;", n+1)); err != nil {
			return fmt.Errorf("schema %d: %w", n+1, storageError(err))
		}
	}
	return nil
}

// Register records the repository for a Git common directory.
func (s *Store) Register(ctx context.Context, commonDir string) (model.RepositoryID, error) {
	if commonDir == "" || !filepath.IsAbs(commonDir) {
		return "", fmt.Errorf("%w: repository directory must be absolute, got %q", model.ErrInvalid, commonDir)
	}
	var id model.RepositoryID
	err := s.transaction(ctx, true, "", func(t *tx) error {
		err := t.conn.QueryRowContext(t.ctx, "SELECT id FROM repositories WHERE common_dir=?", commonDir).Scan(&id)
		if !errors.Is(err, sql.ErrNoRows) {
			return storageError(err)
		}
		id = model.RepositoryID(store.NewID("repo"))
		_, err = t.conn.ExecContext(t.ctx, "INSERT INTO repositories(id, common_dir, created_at) VALUES(?,?,?)", id, commonDir, millis(time.Now()))
		return storageError(err)
	})
	return id, err
}

// View runs fn in a read transaction bound to the repository.
func (s *Store) View(ctx context.Context, repository model.RepositoryID, fn func(store.Reader) error) error {
	if repository == "" || fn == nil {
		return fmt.Errorf("%w: a transaction needs a repository and a function", model.ErrInvalid)
	}
	return s.transaction(ctx, false, repository, func(t *tx) error { return fn(t) })
}

// Update runs fn in a write transaction bound to the repository.
func (s *Store) Update(ctx context.Context, repository model.RepositoryID, fn func(store.Tx) error) error {
	if repository == "" || fn == nil {
		return fmt.Errorf("%w: a transaction needs a repository and a function", model.ErrInvalid)
	}
	return s.transaction(ctx, true, repository, func(t *tx) error { return fn(t) })
}

type inTransaction struct{}

// tx is one transaction on one connection. Its methods are the store.Tx
// contract; a read-only transaction refuses writes.
type tx struct {
	ctx      context.Context
	conn     *sql.Conn
	repo     model.RepositoryID
	writable bool
}

func (s *Store) transaction(ctx context.Context, write bool, repo model.RepositoryID, fn func(*tx) error) error {
	if ctx.Value(inTransaction{}) != nil {
		return fmt.Errorf("%w: nested transaction", model.ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, s.options.OperationTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, inTransaction{}, true)
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return storageError(err)
	}
	defer conn.Close()
	begin := "BEGIN"
	if write {
		begin = "BEGIN IMMEDIATE"
	}
	if _, err := conn.ExecContext(ctx, begin); err != nil {
		return storageError(err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollback, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		if _, err := conn.ExecContext(rollback, "ROLLBACK"); err != nil {
			// A connection whose rollback failed must not be reused.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if repo != "" {
		var exists int
		if err := conn.QueryRowContext(ctx, "SELECT 1 FROM repositories WHERE id=?", repo).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: repository %s is not registered", store.ErrNotFound, repo)
			}
			return storageError(err)
		}
	}
	if err := fn(&tx{ctx: ctx, conn: conn, repo: repo, writable: write}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		mapped := storageError(err)
		if errors.Is(mapped, store.ErrConflict) {
			return mapped
		}
		return fmt.Errorf("%w: %w", store.ErrUncertain, mapped)
	}
	committed = true
	return nil
}

func (t *tx) write() error {
	if !t.writable {
		return fmt.Errorf("%w: write in a read transaction", model.ErrInvalid)
	}
	return nil
}

func (t *tx) exec(query string, args ...any) (sql.Result, error) {
	if err := t.write(); err != nil {
		return nil, err
	}
	result, err := t.conn.ExecContext(t.ctx, query, args...)
	return result, storageError(err)
}

// update runs an UPDATE that must change exactly one row, reporting
// ErrNotFound when it changes none.
func (t *tx) update(what, query string, args ...any) error {
	result, err := t.exec(query, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return storageError(err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", store.ErrNotFound, what)
	}
	return nil
}

// exists reports ErrNotFound unless the repository has the row a query
// selects by key: a reference checked without reading what it names.
func (t *tx) exists(query string, key any) error {
	var found int
	return storageError(t.conn.QueryRowContext(t.ctx, query, t.repo, key).Scan(&found))
}

func busy(err error) bool {
	var coded interface{ Code() int }
	return errors.As(err, &coded) && (coded.Code()&255 == 5 || coded.Code()&255 == 6)
}

func storageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var sqliteErr *sqlitedriver.Error
	if errors.As(err, &sqliteErr) && sqliteErr.Code()&255 == 19 {
		return fmt.Errorf("%w: %w", store.ErrConflict, err)
	}
	return fmt.Errorf("%w: %w", store.ErrUnavailable, err)
}

func millis(t time.Time) int64 { return t.UTC().UnixMilli() }
func fromMillis(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}
func nullableMillis(t *time.Time) any {
	if t == nil {
		return nil
	}
	return millis(*t)
}
func fromNullable(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMillis(v.Int64)
	return &t
}
