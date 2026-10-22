package command

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
)

// TestMain keeps the command tests out of the person's own dockhand
// (cmdtest.Main), running each command line through the root's RunWith.
func TestMain(m *testing.M) { cmdtest.Main(m, RunWith) }
