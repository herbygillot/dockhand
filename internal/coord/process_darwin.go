package coord

import (
	"errors"
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// processStart reads the process's start time from the kernel's process
// table, which is wall-clock time and so differs across reboots.
func processStart(pid int) (string, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	// For a PID with no process, the sysctl succeeds with no data, which
	// SysctlKinfoProc reports as EIO; some releases say ESRCH.
	if errors.Is(err, unix.EIO) || errors.Is(err, syscall.ESRCH) {
		return "", os.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	if info.Proc.P_pid != int32(pid) {
		// The kernel returns an empty record for a PID that does not exist.
		return "", os.ErrNotExist
	}
	start := info.Proc.P_starttime
	return "darwin:" + strconv.FormatInt(int64(start.Sec), 10) + "." + strconv.FormatInt(int64(start.Usec), 10), nil
}
