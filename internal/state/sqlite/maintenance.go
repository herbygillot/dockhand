package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/state"
)

var _ state.Maintenance = (*Store)(nil)

// Check verifies SQLite pages, constraints, and foreign keys in one read snapshot.
// It does not check Git objects, artifact files, VMs, or remote publications.
func (s *Store) Check(ctx context.Context) error {
	return s.transaction(ctx, false, "", func(ctx context.Context, t *transaction) error {
		rows, err := t.conn.QueryContext(ctx, "PRAGMA integrity_check")
		if err != nil {
			return storageError(err)
		}
		var problems []error
		for rows.Next() {
			var message string
			if err = rows.Scan(&message); err != nil {
				break
			}
			if message != "ok" {
				problems = append(problems, fmt.Errorf("%w: %s", state.ErrInvalid, message))
			}
		}
		err = errors.Join(err, rows.Err(), rows.Close(), errors.Join(problems...))
		if err != nil {
			return err
		}
		rows, err = t.conn.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return storageError(err)
		}
		defer rows.Close()
		if rows.Next() {
			return fmt.Errorf("%w: database contains broken foreign-key references", state.ErrInvalid)
		}
		return storageError(rows.Err())
	})
}

// Backup snapshots committed WAL contents through SQLite, then installs the
// checked, synced file without replacing any existing destination.
func (s *Store) Backup(ctx context.Context, destination string) (state.Backup, error) {
	var result state.Backup
	if destination == "" || ctx.Value(transactionKey{}) != nil {
		return result, state.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, s.options.OperationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	path, err := filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	// Check first so ordinary refusal does not create temporary files.
	if _, err = os.Lstat(path); err == nil {
		return result, fmt.Errorf("backup destination already exists: %w", os.ErrExist)
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return result, err
	}
	parentPath, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return result, err
	}
	path = filepath.Join(parentPath, filepath.Base(path))
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if path == s.path+suffix {
			return result, fmt.Errorf("%w: destination overlaps the source database", state.ErrInvalid)
		}
	}
	directory, err := os.MkdirTemp(filepath.Dir(path), ".dockhand-backup-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(directory)
	temporary := filepath.Join(directory, "snapshot.db")
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return result, err
	}
	if err = file.Close(); err != nil {
		return result, err
	}
	// VACUUM INTO cannot run inside a transaction. Its output is a consistent snapshot.
	if _, err = s.db.ExecContext(ctx, "VACUUM main INTO ?", temporary); err != nil {
		return result, storageError(err)
	}
	snapshot, err := Open(ctx, temporary, Options{ReadOnly: true, AllowOlderSchema: true, OperationTimeout: s.options.OperationTimeout})
	if err != nil {
		return result, err
	}
	err = errors.Join(snapshot.Check(ctx), snapshot.Close())
	if err != nil {
		return result, err
	}
	file, err = os.OpenFile(temporary, os.O_RDWR, 0)
	if err != nil {
		return result, err
	}
	info, err := file.Stat()
	err = errors.Join(err, file.Sync(), file.Close())
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	// Hard-link installation is atomic and fails if another backup won the destination.
	if err = os.Link(temporary, path); err != nil {
		return result, err
	}
	result = state.Backup{Path: path, Bytes: info.Size(), CompletedAt: time.Now().UTC()}
	parent, err := os.Open(filepath.Dir(path))
	if err == nil {
		err = errors.Join(parent.Sync(), parent.Close())
	}
	if err != nil {
		return result, fmt.Errorf("backup installed at %s but directory sync failed: %w", path, err)
	}
	return result, nil
}
