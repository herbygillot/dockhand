package cli

import (
	"os"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// TestMain keeps PortIndex generations out of the user's cache directory,
// the tests' scratch out of the user's temporary directory, where gc
// would otherwise sweep the user's stale run roots during a test, and the
// Tart image locks tests take out of the user's ~/.dockhand.
func TestMain(m *testing.M) {
	restoreHome := testsupport.IsolateHome()
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
	// A cold cache seeds from the mirror; tests never reach it.
	os.Setenv("DOCKHAND_INDEX_MIRROR", "http://127.0.0.1:1")
	code := m.Run()
	os.RemoveAll(temp)
	restoreHome()
	os.Exit(code)
}
