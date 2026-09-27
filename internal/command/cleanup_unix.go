//go:build darwin || linux

package command

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/herbygillot/dockhand/internal/engine"
)

// startCleanup starts dockhand clean --automatic on the same checkout and
// database, in a session of its own, writing to cleanup.log beside the
// database; the command that starts it exits without waiting. The tests
// replace it.
var startCleanup = func(options engine.Options) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"clean", "--automatic", "--db", options.Database, "--tree", options.Tree}
	if options.Git != "" {
		args = append(args, "--git", options.Git)
	}
	log, err := os.OpenFile(filepath.Join(filepath.Dir(options.Database), "cleanup.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()
	command := exec.Command(executable, args...)
	command.Stdout, command.Stderr = log, log
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
