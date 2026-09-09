package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/verify"
)

// A MACHINE WITH NO VERIFIER STILL MINTS, and settleRelease must not be
// the thing that stops it.
//
// The composition root documents the road: "no tart at all is
// ErrNoProvider, and the roads narrow their contract around it (a bump
// mints and says unverified)". app.Change already does that — it reads
// the resolver's error, declines to enqueue, mints, and carries the
// advisory. Resolving a platform is an errand on the way to a build, so
// a machine with no build to reach has no platform to resolve; returning
// the resolver's refusal from here turned a mint into exit 33 with no
// branch.
//
// Measured: `bump litestream` on a host with tart off PATH exited 33
// having minted nothing.
func TestSettleReleaseLetsAMachineWithNoProviderThrough(t *testing.T) {
	for _, err := range []error{verify.ErrNoProvider, verify.ErrNoEnvironment} {
		s := &Services{Verifier: func(context.Context) (verify.Verifier, error) { return nil, err }}
		var f intentFlags
		require.NoError(t, settleRelease(context.Background(), s, &f, true),
			"%v is the road's answer to give, not this function's", err)
		assert.True(t, f.release.IsZero(), "and nothing is invented on the way past")
	}
}

// A REFUSAL THAT IS NOT ABOUT HAVING NO BUILD STILL STOPS THE ROAD.
func TestSettleReleaseStillReportsOtherFailures(t *testing.T) {
	boom := errors.New("the listing blew up")
	s := &Services{Verifier: func(context.Context) (verify.Verifier, error) { return nil, boom }}
	var f intentFlags
	assert.ErrorIs(t, settleRelease(context.Background(), s, &f, true), boom)
}
