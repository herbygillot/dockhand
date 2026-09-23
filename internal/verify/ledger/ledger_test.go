package ledger_test

import (
	"context"
	"encoding/json"
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify/ledger"
	"github.com/stretchr/testify/require"
)

func TestLedgerDependencies(t *testing.T) {
	t.Parallel()
	pkg, err := build.Default.ImportDir(".", 0)
	require.NoError(t, err)
	allowed := map[string]bool{
		"github.com/herbygillot/dockhand/internal/record":   true,
		"github.com/herbygillot/dockhand/internal/state":    true,
		"github.com/herbygillot/dockhand/internal/filelock": true,
	}
	for _, path := range pkg.Imports {
		if strings.Contains(strings.Split(path, "/")[0], ".") {
			require.True(t, allowed[path], "the ledger is a leaf under the providers: %s", path)
		}
	}
}

func fixture(t *testing.T) (*sqlite.Store, record.Repository, record.Repository, record.ProviderPool) {
	t.Helper()
	root := t.TempDir()
	store, err := sqlite.Open(t.Context(), filepath.Join(root, "state.db"), sqlite.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	one, err := store.RegisterRepository(t.Context(), filepath.Join(root, "one"))
	require.NoError(t, err)
	two, err := store.RegisterRepository(t.Context(), filepath.Join(root, "two"))
	require.NoError(t, err)
	return store, one, two, record.ProviderPool{ID: "fixture", Scope: "fixture", Directory: filepath.Join(root, "pool"), Capacity: 1}
}

func TestOpenLocksTheRequestAndReadRefusesAnotherRepository(t *testing.T) {
	t.Parallel()
	store, one, two, pool := fixture(t)
	mine := ledger.Ledger{Store: store, Repository: one.ID}
	_, err := ledger.Ledger{}.Open(t.Context(), pool, "request")
	require.ErrorIs(t, err, state.ErrInvalid)
	_, err = mine.Open(t.Context(), pool, "")
	require.ErrorIs(t, err, state.ErrInvalid)

	entry, err := mine.Open(t.Context(), pool, "request")
	require.NoError(t, err)
	require.Equal(t, record.RequestID("request"), entry.ID())
	require.Equal(t, pool.ID, entry.Pool.ID)
	_, err = filelock.TryExisting(t.Context(), ledger.LockPath(pool, "request"), filelock.Exclusive)
	require.ErrorIs(t, err, filelock.ErrBusy, "the request is locked while the entry is open")
	_, err = entry.Read(t.Context())
	require.ErrorIs(t, err, state.ErrNotFound)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	require.NoError(t, entry.Put(t.Context(), record.ProviderExecution{ID: "request", RepositoryID: one.ID, State: record.ExecutionClosed, Result: json.RawMessage(`{"Detail":"refused"}`), CreatedAt: now}))
	row, err := entry.Read(t.Context())
	require.NoError(t, err)
	require.Equal(t, record.ExecutionClosed, row.State)
	require.JSONEq(t, `{"Detail":"refused"}`, string(row.Result), "the result is the provider's, carried as bytes")
	require.NoError(t, entry.View(t.Context(), func(ctx context.Context, r state.ProviderReader) error {
		occupied, err := r.Occupied(ctx)
		require.Empty(t, occupied, "a closed row occupies nothing")
		return err
	}))
	entry.Close()
	held, err := filelock.TryExisting(t.Context(), ledger.LockPath(pool, "request"), filelock.Exclusive)
	require.NoError(t, err, "the lock is free once the entry is closed")
	require.NoError(t, held.Close())

	theirs := ledger.Ledger{Store: store, Repository: two.ID}
	_, err = theirs.Read(t.Context(), pool.ID, "request")
	require.ErrorIs(t, err, state.ErrConflict, "another repository's row is a conflict, not a row")
	other, err := theirs.Open(t.Context(), pool, "request")
	require.NoError(t, err)
	defer other.Close()
	_, err = other.Read(t.Context())
	require.ErrorIs(t, err, state.ErrConflict)
}

func TestCloseUnknownRecordsAClosedRow(t *testing.T) {
	t.Parallel()
	store, one, _, pool := fixture(t)
	mine := ledger.Ledger{Store: store, Repository: one.ID}
	entry, err := mine.Open(t.Context(), pool, "never-seen")
	require.NoError(t, err)
	defer entry.Close()
	now := time.Date(2026, 9, 23, 12, 0, 0, 123456789, time.UTC)
	require.NoError(t, entry.CloseUnknown(t.Context(), now))
	row, err := entry.Read(t.Context())
	require.NoError(t, err)
	require.Equal(t, record.ExecutionClosed, row.State)
	require.Equal(t, one.ID, row.RepositoryID)
	require.Equal(t, now.Truncate(time.Millisecond), row.CreatedAt)
	require.Empty(t, row.Result)
}

func TestReadChunkReportsTheEnd(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "build.log")
	require.NoError(t, os.WriteFile(path, []byte("abcdef"), 0600))
	data, next, eof, err := ledger.ReadChunk(path, 0, 4)
	require.NoError(t, err)
	require.Equal(t, "abcd", string(data))
	require.Equal(t, int64(4), next)
	require.False(t, eof)
	data, next, eof, err = ledger.ReadChunk(path, next, 4)
	require.NoError(t, err)
	require.Equal(t, "ef", string(data))
	require.Equal(t, int64(6), next)
	require.True(t, eof)
	_, next, _, err = ledger.ReadChunk(filepath.Join(t.TempDir(), "missing"), 3, 4)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Equal(t, int64(3), next, "a failed read leaves the offset where it was")
}
