package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// DOCKHAND_HEAPPROFILE's profile is the in-use heap, as runtime/pprof
// writes one, gzipped, replacing what the file held (the roadmap's
// "serve gives back a look's memory once its work ends").
func TestTheHeapProfileIsWritten(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "heap.pprof")
	require.NoError(t, os.WriteFile(file, []byte("an older profile, much longer than its header"), 0o644))
	require.NoError(t, WriteHeapProfile(file))
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, []byte{0x1f, 0x8b}, data[:2], "gzipped, as pprof writes it")
	require.Error(t, WriteHeapProfile(filepath.Join(t.TempDir(), "no", "such", "dir", "heap.pprof")))
}
