package cli

import (
	"os"
	"testing"
)

// TestMain keeps PortIndex generations out of the user's cache directory,
// and the tests' scratch out of the user's temporary directory, where gc
// would otherwise sweep the user's stale run roots during a test.
func TestMain(m *testing.M) {
	temp, err := os.MkdirTemp("", "dockhand-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("TMPDIR", temp)
	cache, err := os.MkdirTemp("", "dockhand-index-cache-")
	if err != nil {
		panic(err)
	}
	os.Setenv("DOCKHAND_INDEX_CACHE", cache)
	code := m.Run()
	os.RemoveAll(temp)
	os.Exit(code)
}
