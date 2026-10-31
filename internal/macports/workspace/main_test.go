package workspace_test

import (
	"os"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// TestMain keeps the tests' scratch, the run root every transient directory
// lives under, out of the user's temporary directory, where the next
// dockhand command would otherwise sweep it as a dead root.
func TestMain(m *testing.M) {
	cleanup := testsupport.IsolateTemp()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
