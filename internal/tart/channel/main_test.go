package channel

import (
	"os"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// TestMain keeps dockhand's SSH keys and control sockets tests make out of
// the person's ~/.dockhand.
func TestMain(m *testing.M) {
	cleanup := testsupport.IsolateHomeKeepingTart()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
