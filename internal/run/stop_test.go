package run

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
	"github.com/herbygillot/dockhand/internal/verify"
)

// mute is a verifier that answers nothing and, crucially, is NOT a
// verify.Stopper. Most providers will be: stopping work while keeping
// the runner alive is a real capability, not a formality.
type mute struct{ verify.Verifier }

// deaf is the same provider with the capability, recording the job it
// was handed so the lease-to-job translation can be asserted.
type deaf struct {
	verify.Verifier
	got verify.Job
}

func (d *deaf) Stop(_ context.Context, job verify.Job) error { d.got = job; return nil }

// A PROVIDER THAT CANNOT STOP SAYS SO, and the difference matters
// because the two answers have opposite consequences for what a person
// is told. "Stopped" and "could not be stopped" are both truthful
// endings for a reap; "stopped" said of a provider that was never asked
// would be a sentence the person's own `dockhand log` contradicts a
// minute later, with a live build still writing to it.
func TestStopRefusesAProviderThatCannotStopWork(t *testing.T) {
	st := newStore(t)
	err := Stop(t.Context(), st, mute{}, record.Attempt{ID: "att", Lease: "lse"})
	assert.ErrorIs(t, err, ErrCannotStop)
}

// AN ATTEMPT HOLDING NO ENVIRONMENT HAS NO BUILD TO STOP, and that is
// not the same answer as a provider that cannot stop one — a caller has
// to be able to tell "there is nothing running" from "there may still
// be", so neither is folded into the other.
func TestStopRefusesAnAttemptWithNoLease(t *testing.T) {
	st := newStore(t)
	// A store that holds a lease, so what is being asserted is the
	// MISSING one and not an empty state ref.
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.PutLease(record.Lease{ID: record.LeaseID{Provider: "tart", ID: "w-1"}, Request: "req-1"})
		return nil
	}))
	err := Stop(t.Context(), st, &deaf{}, record.Attempt{ID: "att", Lease: "gone"})
	assert.ErrorIs(t, err, ErrNoLease)
}

// THE PROVIDER IS ASKED IN ITS OWN VOCABULARY. A lease is how the RECORD
// holds an environment and a job is how the PROVIDER names it; Stop
// exists in this package precisely because the caller holds an attempt
// and the translation between them is the state ref's to make.
func TestStopHandsTheProviderTheJobTheLeaseNames(t *testing.T) {
	st := newStore(t)
	lse := record.Lease{
		ID:      record.LeaseID{Provider: "tart", ID: "dockhand-worker-3", Started: clock},
		Request: "req-9",
	}
	require.NoError(t, st.Amend(t.Context(), func(tx *statestore.Txn) error {
		tx.PutLease(lse)
		return nil
	}))

	prov := &deaf{}
	require.NoError(t, Stop(t.Context(), st, prov, record.Attempt{ID: "att", Lease: lse.Request}))
	assert.Equal(t, "tart", prov.got.Provider)
	assert.Equal(t, "dockhand-worker-3", prov.got.ID)
	assert.Equal(t, "req-9", prov.got.Request)
}
