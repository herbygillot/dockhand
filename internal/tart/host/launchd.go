package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/atomicfile"
	"github.com/herbygillot/dockhand/internal/macos"
)

func (n Machine) launchctl(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "/bin/launchctl", args...)
	cmd.Env = n.Client.Environment()
	cmd.WaitDelay = 2 * time.Second
	// Unfinished commands retain the caller's operation lock after driver death.
	if n.Guard != nil {
		cmd.ExtraFiles = []*os.File{n.Guard}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tart: launchctl: %w: %s", errors.Join(ctx.Err(), err), strings.TrimSpace(stderr.String()))
	}
	return nil
}
func hostLabel(vm string) string { return "org.dockhand2.vm." + vm }
func hostDomain() string         { return "gui/" + strconv.Itoa(os.Getuid()) }
func (n Machine) service(ctx context.Context, target string) (bool, error) {
	err := n.launchctl(ctx, "print", target)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 113 && strings.Contains(err.Error(), "Could not find service") {
		return false, nil
	}
	return false, err
}

// Start runs a VM as a launchd service independent of the calling process.
func (n Machine) Start(ctx context.Context, vm, directory string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("tart: launchd requires macOS")
	}
	bin, err := exec.LookPath(n.Client.Executable)
	if err != nil {
		return err
	}
	path := filepath.Join(directory, "vm.plist")
	data := macos.LaunchdPlist(hostLabel(vm), []string{bin, "run", "--no-graphics", "--no-audio", "--no-clipboard", vm}, filepath.Join(directory, "vm.log"), map[string]string{"TART_HOME": n.Client.Home, "TART_NO_AUTO_PRUNE": "1"})
	if err = atomicfile.Write(path, data, 0600); err != nil {
		return err
	}
	loaded, err := n.service(ctx, hostDomain()+"/"+hostLabel(vm))
	if err != nil {
		return err
	}
	if loaded {
		return nil
	}
	err = n.launchctl(ctx, "bootstrap", hostDomain(), path)
	return err
}

// Stop unloads the service and waits until Tart no longer reports a running VM.
func (n Machine) Stop(ctx context.Context, vm string) error {
	target := hostDomain() + "/" + hostLabel(vm)
	loaded, err := n.service(ctx, target)
	if err != nil {
		return err
	}
	if loaded {
		err = n.launchctl(ctx, "bootout", "--wait", target)
		if err != nil {
			return err
		}
	}
	for {
		loaded, err = n.service(ctx, target)
		if err != nil {
			return err
		}
		_, running, err := n.LocalVM(ctx, vm)
		if err != nil {
			return err
		}
		if !loaded && !running {
			return nil
		}
		if running {
			if _, err = n.run(ctx, nil, "stop", vm); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
