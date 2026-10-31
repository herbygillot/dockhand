package host

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/herbygillot/dockhand/internal/subprocess"
)

// virtualMachineService is the Virtualization framework's XPC service, a
// process for each VM running on the Mac, whoever started it: macOS lets
// a VM go only once it exits, a moment after `tart run` does. Its path is
// read, not a documented interface; nothing documents when macOS frees a
// VM's place (an exception to depending on documented interfaces alone).
const virtualMachineService = "Virtualization.framework/Versions/A/XPCServices/com.apple.Virtualization.VirtualMachine.xpc/"

// VirtualMachines are the processes of the VMs running on the Mac, by
// PID: none where pgrep finds none.
func (n Machine) VirtualMachines(ctx context.Context) ([]int, error) {
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "pgrep", Path: "/usr/bin/pgrep", Args: []string{"-f", virtualMachineService}, Env: n.Client.Environment(), Limit: 1 << 16})
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, field := range strings.Fields(string(result.Output)) {
		if pid, err := strconv.Atoi(field); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// Released waits until each of pids has exited, or within has passed: a
// VM whose `tart run` ended holds its place until its process exits, and
// a check started in the 0.7 s between failed to start its VM (rc2's
// staging, check-827).
func Released(ctx context.Context, pids []int, within time.Duration) {
	deadline := time.Now().Add(within)
	for _, pid := range pids {
		for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
}
