package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

var _ state.ProviderStore = (*Store)(nil)

func (s *Store) RegisterProviderPool(ctx context.Context, p record.ProviderPool) (record.ProviderPool, error) {
	if p.ID == "" || p.Scope == "" || !filepath.IsAbs(p.Directory) || filepath.Clean(p.Directory) != p.Directory || p.Capacity <= 0 {
		return record.ProviderPool{}, state.ErrInvalid
	}
	err := s.transaction(ctx, true, "", func(ctx context.Context, t *transaction) error {
		_, err := t.conn.ExecContext(ctx, "INSERT INTO provider_pools(id,scope,directory,capacity) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING", p.ID, p.Scope, p.Directory, p.Capacity)
		if err != nil {
			return storageError(err)
		}
		var old record.ProviderPool
		err = t.conn.QueryRowContext(ctx, "SELECT id,scope,directory,capacity FROM provider_pools WHERE id=?", p.ID).Scan(&old.ID, &old.Scope, &old.Directory, &old.Capacity)
		if err != nil {
			return storageError(err)
		}
		return immutable(old, p)
	})
	if err != nil {
		return record.ProviderPool{}, err
	}
	return p, nil
}

// SetProviderPoolCapacity changes the one pool fact that is policy rather
// than identity: how many executions may run at once. The pool must exist.
func (s *Store) SetProviderPoolCapacity(ctx context.Context, id string, capacity int) error {
	if id == "" || capacity <= 0 {
		return state.ErrInvalid
	}
	return s.transaction(ctx, true, "", func(ctx context.Context, t *transaction) error {
		result, err := t.conn.ExecContext(ctx, "UPDATE provider_pools SET capacity=? WHERE id=?", capacity, id)
		if err != nil {
			return storageError(err)
		}
		if changed, err := result.RowsAffected(); err != nil {
			return storageError(err)
		} else if changed == 0 {
			return state.ErrNotFound
		}
		return nil
	})
}

func (s *Store) ProviderView(ctx context.Context, pool string, fn func(context.Context, state.ProviderReader) error) error {
	if fn == nil {
		return state.ErrInvalid
	}
	return s.providerTransaction(ctx, false, pool, func(ctx context.Context, t *providerTransaction) error { return fn(ctx, t) })
}
func (s *Store) ProviderUpdate(ctx context.Context, pool string, fn func(context.Context, state.ProviderTx) error) error {
	if fn == nil {
		return state.ErrInvalid
	}
	return s.providerTransaction(ctx, true, pool, func(ctx context.Context, t *providerTransaction) error { return fn(ctx, t) })
}

type providerTransaction struct {
	*transaction
	pool string
}

func (s *Store) providerTransaction(ctx context.Context, write bool, pool string, fn func(context.Context, *providerTransaction) error) error {
	if pool == "" {
		return state.ErrInvalid
	}
	return s.transaction(ctx, write, "", func(ctx context.Context, t *transaction) error {
		var exists int
		if err := t.conn.QueryRowContext(ctx, "SELECT 1 FROM provider_pools WHERE id=?", pool).Scan(&exists); err != nil {
			return storageError(err)
		}
		return fn(ctx, &providerTransaction{t, pool})
	})
}
func (t *providerTransaction) Execution(ctx context.Context, id record.RequestID) (record.ProviderExecution, error) {
	var v record.ProviderExecution
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var attempt, resource sql.NullString
	var created int64
	var payload, result []byte
	err := t.conn.QueryRowContext(ctx, "SELECT id,repository_id,attempt_id,resource,payload,state,occupied,result,created_at FROM provider_executions WHERE pool_id=? AND id=?", t.pool, id).Scan(&v.ID, &v.RepositoryID, &attempt, &resource, &payload, &v.State, &v.Occupied, &result, &created)
	v.Payload, v.Result = payload, result
	v.AttemptID = record.AttemptID(attempt.String)
	v.Resource = resource.String
	v.CreatedAt = fromTime(created)
	return v, storageError(err)
}
func (t *providerTransaction) Occupied(ctx context.Context) ([]record.ProviderExecution, error) {
	ids, err := t.ids(ctx, "SELECT id FROM provider_executions WHERE pool_id=? AND occupied=1 ORDER BY id", []any{t.pool}...)
	return fetch(ctx, ids, err, func(ctx context.Context, id string) (record.ProviderExecution, error) {
		return t.Execution(ctx, record.RequestID(id))
	})
}
func (t *providerTransaction) PutExecution(ctx context.Context, v record.ProviderExecution) error {
	if v.ID == "" || v.RepositoryID == "" || v.CreatedAt.IsZero() || len(v.Payload) > 0 && !json.Valid(v.Payload) || len(v.Result) > 0 && !json.Valid(v.Result) {
		return state.ErrInvalid
	}
	old, err := t.Execution(ctx, v.ID)
	if err == nil {
		same := old
		same.State = v.State
		same.Occupied = v.Occupied
		same.Result = v.Result
		if !reflect.DeepEqual(same, v) {
			return state.ErrConflict
		}
		if (old.State == record.ExecutionClosed || old.State == record.ExecutionReleased) && old.State != v.State && !(old.State == record.ExecutionClosed && v.State == record.ExecutionReleased) {
			return state.ErrConflict
		}
		if old.State == record.ExecutionAdmitted && v.State != record.ExecutionAdmitted && v.State != record.ExecutionReleased {
			return state.ErrConflict
		}
		if !old.Occupied && v.Occupied {
			return state.ErrConflict
		}
		if len(old.Result) > 0 && !reflect.DeepEqual(old.Result, v.Result) {
			return state.ErrConflict
		}
		return t.exec(ctx, "UPDATE provider_executions SET state=?,occupied=?,result=? WHERE pool_id=? AND id=?", v.State, v.Occupied, bytesOrNil(v.Result), t.pool, v.ID)
	}
	if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	if v.State != record.ExecutionReserved && v.State != record.ExecutionClosed {
		return state.ErrInvalid
	}
	return t.exec(ctx, "INSERT INTO provider_executions(id,pool_id,repository_id,attempt_id,resource,payload,state,occupied,result,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", v.ID, t.pool, v.RepositoryID, nullableID(v.AttemptID), nullableID(v.Resource), bytesOrNil(v.Payload), v.State, v.Occupied, bytesOrNil(v.Result), v.CreatedAt.UnixMilli())
}
func bytesOrNil(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func (s *Store) ProviderPool(ctx context.Context, id string) (record.ProviderPool, error) {
	var pool record.ProviderPool
	err := s.transaction(ctx, false, "", func(ctx context.Context, t *transaction) error {
		return storageError(t.conn.QueryRowContext(ctx, "SELECT id,scope,directory,capacity FROM provider_pools WHERE id=?", id).Scan(&pool.ID, &pool.Scope, &pool.Directory, &pool.Capacity))
	})
	return pool, err
}
