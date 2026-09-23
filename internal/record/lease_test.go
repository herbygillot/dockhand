package record

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLeaseEligibilityAndRelease(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	var lease Lease
	require.True(t, lease.Eligible(now), "no claim and no retry is eligible")
	later := now.Add(time.Minute)
	lease.RetryAt = &later
	require.False(t, lease.Eligible(now), "a future retry defers work")
	require.True(t, lease.Eligible(later), "a due retry is eligible")
	lease.Claim = &Claim{Owner: "driver", Generation: 3, ExpiresAt: now.Add(time.Hour)}
	require.False(t, lease.Eligible(later), "a live claim excludes other drivers")
	require.True(t, lease.Eligible(now.Add(2*time.Hour)), "an expired claim no longer excludes")
	lease.ClaimGeneration = 3
	lease.ConsecutiveFailures = 2
	lease.Release()
	require.Nil(t, lease.Claim)
	require.Nil(t, lease.RetryAt)
	require.Equal(t, uint64(3), lease.ClaimGeneration, "the generation outlives the claim")
	require.Equal(t, uint32(2), lease.ConsecutiveFailures, "failures are reset only by expected progress")
}
