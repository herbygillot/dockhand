package patchcheck

import (
	"os"
	"testing"
)

// TestMain keeps the tests' scratch, the run root every transient directory
// lives under, out of the user's temporary directory, where the next
// dockhand command would otherwise sweep it as a dead root.
func TestMain(m *testing.M) {
	temp, err := os.MkdirTemp("", "dockhand-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("TMPDIR", temp)
	code := m.Run()
	os.RemoveAll(temp)
	os.Exit(code)
}
