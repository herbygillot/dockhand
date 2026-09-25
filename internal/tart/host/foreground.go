package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/herbygillot/dockhand/internal/tart"
)

// Foreground is a `tart run` owned by the process that started it, setup's
// guest. It runs in its own process group, so the terminal's Ctrl-C reaches
// dockhand, which stops the VM itself, rather than reaching Tart directly.
type Foreground struct {
	name    string
	machine Machine
	process *os.Process
	exited  chan struct{}
	err     error
}

// StartForeground starts a VM owned by the caller's provisioning process.
// The caller must stop it explicitly, including after cancellation.
func (n Machine) StartForeground(name string) (*Foreground, error) {
	command := exec.Command(n.Client.Executable, "run", "--no-graphics", "--no-audio", "--no-clipboard", name)
	command.Env = n.Client.Environment()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if n.Guard != nil {
		command.ExtraFiles = []*os.File{n.Guard}
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		return nil, err
	}
	run := &Foreground{name: name, machine: n, process: command.Process, exited: make(chan struct{})}
	go func() {
		err := command.Wait()
		if err != nil {
			err = fmt.Errorf("tart: VM %s exited: %w: %s", name, err, strings.TrimSpace(output.String()))
		}
		run.err = err
		close(run.exited)
	}()
	return run, nil
}

// Done is closed when the run exits.
func (f *Foreground) Done() <-chan struct{} { return f.exited }

// Err is how the run exited, once Done is closed: nil for a clean exit.
func (f *Foreground) Err() error {
	<-f.exited
	return f.err
}

// Stop asks Tart to stop the VM, then interrupts the run, then kills it,
// waiting grace for each before the next. "Not running" from Tart is not a
// failure, and neither is a failed `tart stop` when the run still exits,
// which is what stopping means here.
func (f *Foreground) Stop(ctx context.Context, grace time.Duration) error {
	_, stopErr := f.machine.run(ctx, nil, "stop", f.name)
	if errors.Is(stopErr, tart.ErrVMStopped) || errors.Is(stopErr, tart.ErrVMMissing) {
		stopErr = nil
	}
	for _, signal := range []syscall.Signal{0, syscall.SIGINT, syscall.SIGKILL} {
		// The run leads its own group, so the group is the run and whatever
		// it started, and never dockhand.
		if signal != 0 {
			if err := syscall.Kill(-f.process.Pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
				stopErr = errors.Join(stopErr, err)
			}
		}
		select {
		case <-f.exited:
			return nil
		case <-ctx.Done():
			return errors.Join(stopErr, ctx.Err())
		case <-time.After(grace):
		}
	}
	return errors.Join(stopErr, fmt.Errorf("tart: VM %s did not exit after tart stop, SIGINT, and SIGKILL", f.name))
}
