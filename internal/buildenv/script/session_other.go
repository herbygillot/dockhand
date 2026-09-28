//go:build !darwin && !linux

package script

import (
	"os/exec"
	"time"
)

// ownSession has no session to give command here; a cancel kills the shell.
func ownSession(*exec.Cmd, time.Duration) {}
