package local

import (
	"os"
	"testing"

	"github.com/herbygillot/dockhand/internal/buildenv/starter"
)

// TestMain lets the test binary stand in for dockhand as the starter the
// guest program is started through (starter.RunIfAsked).
func TestMain(m *testing.M) {
	starter.RunIfAsked()
	os.Exit(m.Run())
}
