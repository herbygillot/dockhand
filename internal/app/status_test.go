package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// A PURGED CHECKOUT IS AN ORDINARY THING FOR STATUS TO FIND.
//
// `purge` removes the state ref by design, and the very next `status`
// exited 1 — the band of last resort, "nothing here says whose fault
// this is" — for a checkout in a completely normal condition.
//
// Status.Run already read the lifecycle through ReadOrEmpty for exactly
// this reason, and it STILL exited 1, because lease.Outstanding runs
// afterwards and reads strictly. The split gave it away: `status
// --no-update` returns before that call and exited 0 with the right
// sentence, a second apart on the same checkout.
//
// Outstanding's strictness is a ruling and stays: the reconciler on the
// other side destroys provider resources, and with an empty state every
// live worker looks untracked. Status is the caller that ruling is not
// about.
func TestStatusOnACheckoutWithNoStateRefIsNotAFailure(t *testing.T) {
	repo, st := fixture(t)
	op := Status{
		Repo: repo, Ledger: ledger.Open(repo), State: st, Local: quiet{},
		Verifier: has(&verifytest.Fake{}), Me: me(),
		Residency: Residency{State: NoDispatcher}, Now: now,
	}
	res, err := op.Run(t.Context(), StatusRequest{})
	require.NoError(t, err, "a checkout with no state ref has nothing in flight, which is an answer")
	assert.Empty(t, res.State.Changes)
	assert.Empty(t, res.Obligations, "and no leases to account for")
}
