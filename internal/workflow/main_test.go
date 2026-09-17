package workflow_test

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
	// Every fixture repository must ignore the developer's Git configuration;
	// set once here so fixtures can run in parallel.
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	code := m.Run()
	os.RemoveAll(cache)
	os.Exit(code)
}
