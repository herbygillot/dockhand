package app_test

import (
	"os"
	"testing"
)

// TestMain keeps PortIndex generations out of the user's cache directory.
func TestMain(m *testing.M) {
	cache, err := os.MkdirTemp("", "dockhand-index-cache-")
	if err != nil {
		panic(err)
	}
	os.Setenv("DOCKHAND_INDEX_CACHE", cache)
	code := m.Run()
	os.RemoveAll(cache)
	os.Exit(code)
}
