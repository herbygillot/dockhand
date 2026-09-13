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

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
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

const schemaVersion = 7
const applicationID = 0x44484e44

type Options struct {
	ReadOnly         bool
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
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") || options.BusyTimeout < 0 || options.OperationTimeout < 0 {
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
	if !options.ReadOnly {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, fmt.Errorf("%w: %w", state.ErrUnavailable, err)
		}
	}
	if resolved, e := filepath.EvalSymlinks(path); e == nil {
		path = resolved
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %w", state.ErrUnavailable, e)
	} else {
		if options.ReadOnly {
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
		if appID == applicationID && (version == schemaVersion || version >= 1 && version < schemaVersion && !s.options.ReadOnly) {
			return nil
		}
		if appID != 0 || version != 0 || s.options.ReadOnly {
			return state.ErrSchema
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
		if appID == applicationID && version == schemaVersion {
			return nil
		}
		if appID == applicationID && version >= 1 && version < schemaVersion && !s.options.ReadOnly {
			if version == 1 {
				if _, err := t.conn.ExecContext(ctx, providerSchema); err != nil {
					return storageError(err)
				}
			}
			if version < 3 {
				if err := migratePreparation(ctx, t); err != nil {
					return err
				}
			}
			if version < 4 {
				if _, err := t.conn.ExecContext(ctx, releaseSchema); err != nil {
					return storageError(err)
				}
			}
			if version < 5 {
				if _, err := t.conn.ExecContext(ctx, verificationSchema); err != nil {
					return storageError(err)
				}
			}
			if version < 6 {
				if _, err := t.conn.ExecContext(ctx, publicationSchema); err != nil {
					return storageError(err)
				}
			}
			_, err := t.conn.ExecContext(ctx, imageSchema)
			return storageError(err)
		}
		if appID != 0 || version != 0 || s.options.ReadOnly {
			return state.ErrSchema
		}
		var count int
		if err := t.conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&count); err != nil {
			return storageError(err)
		}
		if count != 0 {
			return fmt.Errorf("%w: not a Dockhand database", state.ErrSchema)
		}
		if _, err := t.conn.ExecContext(ctx, initialSchema+providerSchema+fmt.Sprintf("PRAGMA application_id=%d;", applicationID)); err != nil {
			return storageError(err)
		}
		if err := migratePreparation(ctx, t); err != nil {
			return err
		}
		_, err := t.conn.ExecContext(ctx, releaseSchema+verificationSchema+publicationSchema+imageSchema)
		return storageError(err)
	})
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
	if _, err := t.conn.ExecContext(ctx, preparationSchema); err != nil {
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
	_, err = t.conn.ExecContext(ctx, "PRAGMA defer_foreign_keys=OFF; PRAGMA user_version=3;")
	return storageError(err)
}
