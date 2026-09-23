// Package ledger is the execution ledger a verification provider keeps in
// the shared store: one row per request, reserved, admitted, closed, or
// released, scoped to one repository and one pool, and edited under a
// per-request file lock so that drivers in different processes take
// turns. The rule it exists to keep is that a request the ledger has
// closed refuses every later submit; what a provider answers when it
// refuses is the provider's own, as is everything in the row's payload
// and result.
package ledger

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// Ledger is a provider's execution rows for one repository.
type Ledger struct {
	Store      state.ProviderStore
	Repository record.RepositoryID
}

// Read is the request's row in the pool, and ErrConflict for a row another
// repository wrote. It takes no lock: the row is read as it stands.
func (l Ledger) Read(ctx context.Context, pool string, id record.RequestID) (record.ProviderExecution, error) {
	var v record.ProviderExecution
	err := l.Store.ProviderView(ctx, pool, func(ctx context.Context, r state.ProviderReader) error {
		var err error
		v, err = r.Execution(ctx, id)
		return err
	})
	if err == nil && v.RepositoryID != l.Repository {
		return record.ProviderExecution{}, state.ErrConflict
	}
	return v, err
}

// Open registers the pool and takes the request's lock, waiting for a
// holder in another process to finish. The entry reads and writes the
// request's row until Close.
func (l Ledger) Open(ctx context.Context, pool record.ProviderPool, id record.RequestID) (*Entry, error) {
	if l.Store == nil || l.Repository == "" || id == "" {
		return nil, state.ErrInvalid
	}
	registered, err := l.Store.RegisterProviderPool(ctx, pool)
	if err != nil {
		return nil, err
	}
	lock, err := filelock.Acquire(ctx, LockPath(registered, id), filelock.Exclusive)
	if err != nil {
		return nil, err
	}
	return &Entry{Pool: registered, Lock: lock, ledger: l, id: id}, nil
}

// LockPath names the request's lock file: in locks under the pool's
// directory, hashed as every lock key is.
func LockPath(pool record.ProviderPool, id record.RequestID) string {
	return filelock.Path(filepath.Join(pool.Directory, "locks"), string(id))
}

// Entry is one request's row, held under its lock until Close. Pool is
// the pool as registered, and Lock the held file, for a provider that
// hands it to a child process.
type Entry struct {
	Pool   record.ProviderPool
	Lock   *os.File
	ledger Ledger
	id     record.RequestID
}

// ID is the request the entry is for.
func (e *Entry) ID() record.RequestID { return e.id }

// Read is the request's row.
func (e *Entry) Read(ctx context.Context) (record.ProviderExecution, error) {
	return e.ledger.Read(ctx, e.Pool.ID, e.id)
}

// Put writes the row.
func (e *Entry) Put(ctx context.Context, v record.ProviderExecution) error {
	return e.Update(ctx, func(ctx context.Context, tx state.ProviderTx) error { return tx.PutExecution(ctx, v) })
}

// View reads the pool in one transaction, for a check over other rows.
func (e *Entry) View(ctx context.Context, fn func(context.Context, state.ProviderReader) error) error {
	return e.ledger.Store.ProviderView(ctx, e.Pool.ID, fn)
}

// Update writes the pool in one transaction, for a write with a
// precondition over other rows.
func (e *Entry) Update(ctx context.Context, fn func(context.Context, state.ProviderTx) error) error {
	return e.ledger.Store.ProviderUpdate(ctx, e.Pool.ID, fn)
}

// CloseUnknown records the closed row for a request the ledger has never
// seen, so that no later submit can start it.
func (e *Entry) CloseUnknown(ctx context.Context, now time.Time) error {
	return e.Put(ctx, record.ProviderExecution{ID: e.id, RepositoryID: e.ledger.Repository, State: record.ExecutionClosed, CreatedAt: now.UTC().Truncate(time.Millisecond)})
}

// Close releases the lock.
func (e *Entry) Close() { _ = e.Lock.Close() }

// ReadChunk reads up to limit bytes of a file from offset: the bytes, the
// offset after them, and whether the file ended within them.
func ReadChunk(path string, offset int64, limit int) (data []byte, next int64, eof bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, offset, false, err
	}
	defer file.Close()
	data = make([]byte, limit)
	n, err := file.ReadAt(data, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, offset, false, err
	}
	return data[:n], offset + int64(n), errors.Is(err, io.EOF), nil
}
