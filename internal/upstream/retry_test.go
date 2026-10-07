package upstream

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
)

// A discovery a rate limit stopped carries when it lifts, however wrapped
// (the rc6 full stage, D-N4).
func TestARateLimitCarriesWhenItLifts(t *testing.T) {
	lifts := time.Date(2026, 10, 7, 3, 24, 0, 0, time.UTC)
	limited := &forge.RateLimitError{RetryAt: lifts, Err: errors.New("GitHub's rate limit resets in 18 minutes")}
	require.Equal(t, lifts, retryAt(fmt.Errorf("listing tags: %w", limited)))
	require.True(t, retryAt(errors.New("no forge")).IsZero())
}
