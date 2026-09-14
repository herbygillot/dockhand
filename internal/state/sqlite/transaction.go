package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	sqlitedriver "modernc.org/sqlite"
	"time"
)

func (s *Store) View(ctx context.Context, repository record.RepositoryID, fn func(context.Context, state.Reader) error) error {
	if repository == "" || fn == nil {
		return state.ErrInvalid
	}
	return s.transaction(ctx, false, repository, func(ctx context.Context, t *transaction) error { return fn(ctx, t) })
}
func (s *Store) Update(ctx context.Context, repository record.RepositoryID, fn func(context.Context, state.Tx) error) error {
	if repository == "" || fn == nil {
		return state.ErrInvalid
	}
	return s.transaction(ctx, true, repository, func(ctx context.Context, t *transaction) error { return fn(ctx, t) })
}

type transactionKey struct{}
type transaction struct {
	conn     *sql.Conn
	repo     record.RepositoryID
	ctx      context.Context
	writable bool
	active   bool
}

func (s *Store) transaction(ctx context.Context, write bool, repo record.RepositoryID, fn func(context.Context, *transaction) error) error {
	if ctx.Value(transactionKey{}) != nil {
		return fmt.Errorf("%w: nested transaction", state.ErrInvalid)
	}
	if write && s.options.ReadOnly {
		return state.ErrReadOnly
	}
	ctx, cancel := context.WithTimeout(ctx, s.options.OperationTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, transactionKey{}, true)
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return storageError(err)
	}
	defer conn.Close()
	begin := "BEGIN"
	if write {
		begin = "BEGIN IMMEDIATE"
	}
	if _, err = conn.ExecContext(ctx, begin); err != nil {
		return storageError(err)
	}
	t := &transaction{conn: conn, repo: repo, ctx: ctx, writable: write, active: true}
	committed := false
	defer func() {
		t.active = false
		if !committed {
			rollbackCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer stop()
			if _, e := conn.ExecContext(rollbackCtx, "ROLLBACK"); e != nil {
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			}
		}
	}()
	if repo != "" {
		var exists int
		if err = conn.QueryRowContext(ctx, "SELECT 1 FROM repositories WHERE id=?", repo).Scan(&exists); err != nil {
			return storageError(err)
		}
	}
	if err = fn(ctx, t); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		mapped := storageError(err)
		if errors.Is(mapped, state.ErrConflict) {
			return mapped
		}
		return fmt.Errorf("%w: %w", state.ErrUncertain, mapped)
	}
	committed = true
	return nil
}
func (t *transaction) check(ctx context.Context, write bool) error {
	if !t.active {
		return fmt.Errorf("%w: transaction ended", state.ErrInvalid)
	}
	if err := t.ctx.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if write && !t.writable {
		return state.ErrReadOnly
	}
	return nil
}
func storageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return state.ErrNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var sqliteErr *sqlitedriver.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() & 255 {
		case 19:
			return fmt.Errorf("%w: %w", state.ErrConflict, err)
		case 8:
			return fmt.Errorf("%w: %w", state.ErrReadOnly, err)
		}
	}
	return fmt.Errorf("%w: %w", state.ErrUnavailable, err)
}
func fromTime(ms int64) time.Time { return time.UnixMilli(ms).UTC() }
func nullableTime(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.UTC().UnixMilli()
}
func scanTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	n := fromTime(v.Int64)
	return &n
}
func nullableID[T ~string](v T) any {
	if v == "" {
		return nil
	}
	return string(v)
}
