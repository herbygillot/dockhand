package workflow_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/stretchr/testify/require"
)

// Test snapshots simplify assertions across a handful of fixture records. They
// are not used by the workflow or the storage contract.
type fixtureState struct {
	Jobs      map[record.JobID]record.Job
	Requests  map[record.RequestID]record.JobID
	Changes   map[record.ChangeID]record.Change
	Revisions map[record.RevisionID]record.Revision
	Attempts  map[record.AttemptID]record.Attempt
	Controls  map[record.RequestID]record.ControlRequest
	Resources map[record.ResourceID]record.Resource
}
type fixtureTx struct{ State fixtureState }
type fixtureSnapshot struct {
	Version string
	State   fixtureState
}

func readFixture(ctx context.Context, r state.Reader) (fixtureState, error) {
	s := fixtureState{Jobs: map[record.JobID]record.Job{}, Requests: map[record.RequestID]record.JobID{}, Changes: map[record.ChangeID]record.Change{}, Revisions: map[record.RevisionID]record.Revision{}, Attempts: map[record.AttemptID]record.Attempt{}, Controls: map[record.RequestID]record.ControlRequest{}, Resources: map[record.ResourceID]record.Resource{}}
	jobs, err := r.Jobs(ctx, state.Query{})
	if err != nil {
		return s, err
	}
	for _, v := range jobs {
		s.Jobs[v.ID] = v
		s.Requests[v.RequestID] = v.ID
		attempts, e := r.AttemptsForJob(ctx, v.ID)
		if e != nil {
			return s, e
		}
		for _, a := range attempts {
			s.Attempts[a.ID] = a
		}
	}
	changes, err := r.Changes(ctx, state.Query{})
	if err != nil {
		return s, err
	}
	for _, v := range changes {
		s.Changes[v.ID] = v
	}
	revisions, err := r.Revisions(ctx, state.Query{})
	if err != nil {
		return s, err
	}
	for _, v := range revisions {
		s.Revisions[v.ID] = v
	}
	controls, err := r.Controls(ctx, state.Query{})
	if err != nil {
		return s, err
	}
	for _, v := range controls {
		s.Controls[v.ID] = v
	}
	resources, err := r.Resources(ctx, state.Query{})
	if err != nil {
		return s, err
	}
	for _, v := range resources {
		s.Resources[v.ID] = v
	}
	return s, nil
}
func (f *fixture) snapshot(ctx context.Context) (fixtureSnapshot, error) {
	var s fixtureState
	err := f.store.View(ctx, f.repository, func(ctx context.Context, r state.Reader) error {
		var err error
		s, err = readFixture(ctx, r)
		return err
	})
	b, _ := json.Marshal(s)
	return fixtureSnapshot{Version: fmt.Sprintf("%x", sha256.Sum256(b)), State: s}, err
}
func (f *fixture) mutate(ctx context.Context, fn func(context.Context, *fixtureTx) error) error {
	return f.store.Update(ctx, f.repository, func(ctx context.Context, tx state.Tx) error {
		old, err := readFixture(ctx, tx)
		if err != nil {
			return err
		}
		b, _ := json.Marshal(old)
		var next fixtureState
		if err = json.Unmarshal(b, &next); err != nil {
			return err
		}
		wrapper := fixtureTx{next}
		if err = fn(ctx, &wrapper); err != nil {
			return err
		}
		next = wrapper.State
		for id, v := range next.Changes {
			if !reflect.DeepEqual(old.Changes[id], v) {
				if err = tx.PutChange(ctx, v); err != nil {
					return err
				}
			}
		}
		for id, v := range next.Revisions {
			if !reflect.DeepEqual(old.Revisions[id], v) {
				if err = tx.PutRevision(ctx, v); err != nil {
					return err
				}
			}
		}
		for id, v := range next.Jobs {
			if !reflect.DeepEqual(old.Jobs[id], v) {
				if err = tx.PutJob(ctx, v); err != nil {
					return err
				}
			}
		}
		for id, v := range next.Attempts {
			if !reflect.DeepEqual(old.Attempts[id], v) {
				if err = tx.PutAttempt(ctx, v); err != nil {
					return err
				}
			}
		}
		for id, v := range next.Resources {
			if !reflect.DeepEqual(old.Resources[id], v) {
				if err = tx.PutResource(ctx, v); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

type heldWriter struct {
	once    sync.Once
	release chan struct{}
	done    chan error
}

func (h *heldWriter) Close() error {
	var err error
	h.once.Do(func() { close(h.release); err = <-h.done })
	return err
}
func (f *fixture) holdWriter(t *testing.T) *heldWriter {
	t.Helper()
	h := &heldWriter{release: make(chan struct{}), done: make(chan error, 1)}
	started := make(chan struct{})
	go func() {
		h.done <- f.store.Update(t.Context(), f.repository, func(context.Context, state.Tx) error { close(started); <-h.release; return nil })
	}()
	receive(t, started)
	t.Cleanup(func() { h.Close() })
	return h
}
func (f *fixture) closed(t *testing.T, id record.AttemptID) []record.RequestID {
	t.Helper()
	ids := []record.RequestID{}
	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, r state.Reader) error {
		ss, err := r.SubmissionsForAttempt(ctx, id)
		for _, s := range ss {
			if s.ClosedAt != nil {
				ids = append(ids, s.ID)
			}
		}
		return err
	}))
	return ids
}
