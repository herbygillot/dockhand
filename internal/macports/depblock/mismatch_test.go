package depblock

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Mismatched compares entries in any order, and names the first of those
// that differ by crate and version, with how (#163).
func TestMismatchedNamesTheFirstDifference(t *testing.T) {
	t.Parallel()
	b, c := "bbbb", "cccc"
	lock := []string{"linux-raw-sys", "0.4.15", b, "linux-raw-sys", "0.12.1", b, "rand", "0.8.8", b}
	reordered := []string{"linux-raw-sys", "0.12.1", b, "rand", "0.8.8", b, "linux-raw-sys", "0.4.15", b}
	require.True(t, Equivalent(Cargo, reordered, lock))
	mismatch, err := Mismatched(Cargo, reordered, lock)
	require.NoError(t, err)
	require.Zero(t, mismatch.Count)

	mismatch, err = Mismatched(Cargo, []string{"linux-raw-sys", "0.12.1", b, "rand", "0.8.8", c, "zerocopy", "0.8.0", b}, lock)
	require.NoError(t, err)
	require.Equal(t, Mismatch{Count: 3, First: "linux-raw-sys 0.4.15, missing"}, mismatch)
	mismatch, err = Mismatched(Cargo, []string{"linux-raw-sys", "0.4.15", b, "linux-raw-sys", "0.12.1", b, "rand", "0.8.8", c}, lock)
	require.NoError(t, err)
	require.Equal(t, Mismatch{Count: 1, First: "rand 0.8.8, checksum differs"}, mismatch)
	mismatch, err = Mismatched(Cargo, append(lock[:len(lock):len(lock)], "zerocopy", "0.8.0", b), lock)
	require.NoError(t, err)
	require.Equal(t, Mismatch{Count: 1, First: "zerocopy 0.8.0, unexpected"}, mismatch)

	_, err = Mismatched(Cargo, []string{"rand", "0.8.8"}, lock)
	require.Error(t, err, "an incomplete row")
}
