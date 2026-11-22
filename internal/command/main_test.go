package command

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/config"
)

// TestMain keeps the command tests out of the person's own dockhand
// (cmdtest.Main), running each command line through the root's RunWith.
func TestMain(m *testing.M) { cmdtest.Main(m, RunWith) }

// The command tests run with none of the variables dockhand reads set, by
// config.Environment's one list: a hand-written list missed
// DOCKHAND_GITHUB_LOG (#294).
func TestTheCommandTestsSetNoVariableDockhandReads(t *testing.T) {
	t.Parallel()
	for _, variable := range config.Environment {
		_, set := os.LookupEnv(variable.Name)
		require.False(t, set, variable.Name)
	}
}
