package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/store"
)

// ErrBackupExists is a backup's refusal of a file already there: a backup
// never replaces one.
var ErrBackupExists = errors.New("a file is already there")

// Backup copies the dockhand database at path to into, whole or not at
// all, the way a migration keeps its copy (copyWhole): a consistent copy
// while other processes write, since a reader doesn't block writers in WAL
// mode, compacted, a plain SQLite file with no -wal, readable only by its
// owner. It opens the file alone, as any SQLite reader does: no
// migration, no session, and no record written, so a build backs up a
// database of an older schema before it migrates it, and one of a newer
// schema it can't otherwise open. A
// file that isn't a dockhand v3 database is refused by name, as Open
// refuses one, and so is an into that exists. into's directory is made
// where it's missing, as the database's own is.
func Backup(ctx context.Context, path, into string, options Options) (store.Backup, error) {
	if options.BusyTimeout <= 0 {
		options.BusyTimeout = 5 * time.Second
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return store.Backup{}, err
	}
	if into, err = filepath.Abs(into); err != nil {
		return store.Backup{}, err
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return store.Backup{}, fmt.Errorf("%w: there is no database at %s; nothing was copied", store.ErrUnavailable, path)
	case err != nil:
		return store.Backup{}, fmt.Errorf("%w: %w", store.ErrUnavailable, err)
	case !info.Mode().IsRegular():
		return store.Backup{}, fmt.Errorf("%w: %s is not a dockhand database", store.ErrSchema, path)
	}
	if _, err := os.Lstat(into); err == nil {
		return store.Backup{}, fmt.Errorf("%w: %s; a backup never replaces a file, so name another, or a directory", ErrBackupExists, into)
	}
	params := url.Values{"mode": {"rw"}, "_pragma": {fmt.Sprintf("busy_timeout(%d)", max(1, options.BusyTimeout.Milliseconds()))}}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: params.Encode()}).String())
	if err != nil {
		return store.Backup{}, storageError(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	version, app, err := schemaOf(ctx, db)
	switch {
	case err != nil && corrupt(err):
		return store.Backup{}, fmt.Errorf("%w: %s is not a dockhand database", store.ErrSchema, path)
	case err != nil:
		return store.Backup{}, err
	case app == v2ApplicationID:
		return store.Backup{}, fmt.Errorf("%w: %s is a dockhand v2 database, which dockhand backup doesn't copy", store.ErrSchema, path)
	case app != applicationID:
		return store.Backup{}, fmt.Errorf("%w: %s is not a dockhand database", store.ErrSchema, path)
	}
	made := store.Backup{Source: path, Copy: into, Schema: version}
	if build, started, ok := lastBuild(ctx, db); ok {
		made.Build, made.OpenedAt = build, started
	}
	if err := os.MkdirAll(filepath.Dir(into), 0o700); err != nil {
		return store.Backup{}, fmt.Errorf("%w: %w; nothing was copied", store.ErrUnavailable, err)
	}
	placed, err := copyWhole(ctx, db, into)
	if err != nil {
		return store.Backup{}, fmt.Errorf("%w: copying %s to %s: %w; nothing was copied", store.ErrUnavailable, path, into, err)
	}
	if !placed {
		return store.Backup{}, fmt.Errorf("%w: %s, put there while the copy was made; a backup never replaces a file, and nothing was copied", ErrBackupExists, into)
	}
	if info, err := os.Stat(into); err == nil {
		made.Bytes = info.Size()
	}
	return made, nil
}
