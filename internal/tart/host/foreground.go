package host

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// StartForeground starts a VM owned by the caller's provisioning process.
// The caller must stop it explicitly, including after cancellation.
func (n Machine) StartForeground(name string) (<-chan error, error) {
	command := exec.Command(n.Client.Executable, "run", "--no-graphics", "--no-audio", "--no-clipboard", name)
	command.Env = n.Client.Environment()
	if n.Guard != nil {
		command.ExtraFiles = []*os.File{n.Guard}
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		err := command.Wait()
		if err != nil {
			err = fmt.Errorf("tart: VM %s exited: %w: %s", name, err, strings.TrimSpace(output.String()))
		}
		done <- err
		close(done)
	}()
	return done, nil
}
