//go:build darwin || linux

package script

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// ownSession runs command in a session of its own, which it leads, with no
// controlling terminal. Canceling it interrupts the whole session, so a
// port it started stops, releasing MacPorts' lock, rather than being left
// behind as the shell alone is killed. What still runs after grace is
// killed. Without a terminal, a sudo that would prompt fails at once,
// saying so in the log, as it would under serve, rather than stopping the
// build to wait for input no one can give.
func ownSession(command *exec.Cmd, grace time.Duration) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	command.Cancel = func() error {
		// The session leads its own process group, and the group is the
		// script and what it started, never dockhand.
		group := -command.Process.Pid
		go func() {
			time.Sleep(grace)
			_ = syscall.Kill(group, syscall.SIGKILL)
		}()
		if err := syscall.Kill(group, syscall.SIGINT); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
}
