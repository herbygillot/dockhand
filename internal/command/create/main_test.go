package create_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/command"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
)

// TestMain runs the tests' command lines through the root (cmdtest.Main).
func TestMain(m *testing.M) { cmdtest.Main(m, command.RunWith) }
