package upstream

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A commit pin's version is read as its commit's date and pieces of its
// hash, and given to a new commit the same way: wezterm's
// 20261005-372548-295b0b25 is the commit's date, then its hash's first six
// characters and the eight after them (the roadmap's v0.5.0 part 3, step
// 2). A version that reads as neither is the person's to name.
func TestAPinnedVersionFollowsItsCommitsDateAndHash(t *testing.T) {
	t.Parallel()
	old, now := "372548295b0b25c9a5400f0ea56f0e89f47e0524", "e2a98e1b8af156988ad0ac778658ac27b60a55fb"
	was, is := time.Date(2026, 10, 5, 18, 0, 0, 0, time.UTC), time.Date(2026, 10, 10, 2, 0, 0, 0, time.UTC)
	version, ok := pinnedVersion("20261005-372548-295b0b25", old, was, now, is)
	require.True(t, ok)
	require.Equal(t, "20261010-e2a98e-1b8af156", version)
	version, ok = pinnedVersion("0.0.0-2026.10.05-372548295b0b", old, was, now, is)
	require.True(t, ok)
	require.Equal(t, "0.0.0-2026.10.10-e2a98e1b8af1", version)
	_, ok = pinnedVersion("1.2.3", old, was, now, is)
	require.False(t, ok, "neither date nor hash")
	_, ok = pinnedVersion("372548295b0b", old, was, now, is)
	require.False(t, ok, "a hash with no date")
}
