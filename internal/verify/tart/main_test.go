package tart

import (
	"os"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// TestMain keeps the Tart image locks tests take out of the person's
// ~/.dockhand.
func TestMain(m *testing.M) {
	cleanup := testsupport.IsolateHomeKeepingTart()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
