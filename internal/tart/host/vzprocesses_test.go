package host

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Released waits for a process to exit, within its bound.
func TestReleasedWaitsForAProcessToExit(t *testing.T) {
	t.Parallel()
	sleeping := exec.Command("/bin/sleep", "0.3")
	require.NoError(t, sleeping.Start())
	go func() { _ = sleeping.Wait() }()
	start := time.Now()
	Released(t.Context(), []int{sleeping.Process.Pid}, 5*time.Second)
	require.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond, "it waited")
	start = time.Now()
	Released(t.Context(), []int{os.Getpid()}, 300*time.Millisecond)
	require.Less(t, time.Since(start), 2*time.Second, "within its bound")
	pids, err := Machine{}.VirtualMachines(t.Context())
	require.NoError(t, err, "none running reads as none")
	_ = pids
}
