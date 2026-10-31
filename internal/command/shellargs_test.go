package command_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
)

// shell refuses what it can't run before it opens anything: two checks
// named, a command with --test, and no command without a terminal to
// attach, where no command test drove it (the test analysis's step b).
func TestShellRefusesWhatItCantRun(t *testing.T) {
	t.Parallel()
	cmdtest.NewWorld(t)
	_, _, err := cmdtest.Dockhand(t, "shell", "check-1", "check-2")
	require.EqualError(t, err, "name one check, and put the command after --")
	_, _, err = cmdtest.Dockhand(t, "shell", "check-1", "--test", "--", "ls")
	require.EqualError(t, err, "--test runs the port's test phase; name no command with it")
	_, _, err = cmdtest.Dockhand(t, "shell", "check-1")
	require.EqualError(t, err, "without a terminal, name the command to run after --, as dockhand shell check-12 -- ls")
}
