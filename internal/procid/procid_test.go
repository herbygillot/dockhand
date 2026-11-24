package procid

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A start time this Mac read is compared to the second by the root stop;
// one read elsewhere has no seconds to give.
func TestAStartTimesSecondsAreItsWholeSeconds(t *testing.T) {
	t.Parallel()
	seconds, ok := Seconds("darwin:1763900000.123456")
	require.True(t, ok)
	require.Equal(t, "1763900000", seconds)
	for _, start := range []string{"", "linux:abc:123", "darwin:", "1763900000.1"} {
		_, ok := Seconds(start)
		require.False(t, ok, start)
	}
}
