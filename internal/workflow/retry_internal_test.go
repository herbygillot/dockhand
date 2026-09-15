package workflow

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestFailureBackoffCapsAndSpreadsJobs(t *testing.T) {
	now := time.Now()
	c := cycle{engine: &Engine{Now: func() time.Time { return now }}, retry: time.Second}
	a, b := uint32(1), uint32(1)
	first := c.failureDeadline("job-a", &a, nil)
	second := c.failureDeadline("job-b", &b, nil)
	require.NotEqual(t, first, second)
	require.GreaterOrEqual(t, first.Sub(now), 2*time.Second-time.Millisecond)
	failures := ^uint32(0)
	deadline := c.failureDeadline("job-a", &failures, nil)
	require.Equal(t, ^uint32(0), failures)
	require.WithinDuration(t, now.Add(5*time.Minute), deadline, time.Millisecond)
}
