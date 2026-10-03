//go:build acceptance

package failpoint_test

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/failpoint"
)

// In an acceptance build, a step DOCKHAND_FAILPOINT names kills the
// process there, as kill -9 would, and any other step passes.
func TestAFailpointKillsAtItsStep(t *testing.T) {
	require.True(t, failpoint.Enabled)
	if os.Getenv("FAILPOINT_CHILD") == "1" {
		failpoint.Hit("update.prepared")
		os.Exit(0)
	}
	for step, killed := range map[string]bool{"update.prepared:kill": true, "tidy.prepared:kill": false, "update.prepared:pause": false} {
		child := exec.Command(os.Args[0], "-test.run", "^TestAFailpointKillsAtItsStep$")
		child.Env = append(os.Environ(), "FAILPOINT_CHILD=1", "DOCKHAND_FAILPOINT="+step)
		err := child.Run()
		var exit *exec.ExitError
		if killed {
			require.True(t, errors.As(err, &exit), step)
			status, _ := exit.Sys().(syscall.WaitStatus)
			require.Equal(t, syscall.SIGKILL, status.Signal(), step)
		} else {
			require.NoError(t, err, step)
		}
	}
}
