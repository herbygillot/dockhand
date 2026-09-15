package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/tart"
)

// Machine carries the runtime and optional operation lock inherited by commands.
type Machine struct {
	Client tart.Client
	Guard  *os.File
}

func (n Machine) run(ctx context.Context, imageGuard *os.File, args ...string) ([]byte, error) {
	return n.Client.Run(ctx, tart.RunOptions{ExtraFiles: []*os.File{n.Guard, imageGuard}}, args...)
}
func (n Machine) Version(ctx context.Context) (string, error) {
	out, err := n.run(ctx, nil, "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (n Machine) LocalVM(ctx context.Context, name string) (exists, running bool, err error) {
	vms, err := n.Client.Images(ctx, tart.RunOptions{ExtraFiles: []*os.File{n.Guard}})
	if err != nil {
		return false, false, err
	}
	for _, vm := range vms {
		if vm.Name == name && vm.Source == "local" {
			return true, vm.Running, nil
		}
	}
	// An incomplete clone is an owned resource even if Tart cannot list it.
	_, err = os.Lstat(filepath.Join(n.Client.Home, "vms", name))
	if err == nil {
		return true, false, fmt.Errorf("tart: VM %s exists but could not be listed", name)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, false, err
	}
	return false, false, nil
}
func (n Machine) Running(ctx context.Context) ([]string, error) {
	vms, err := n.Client.Images(ctx, tart.RunOptions{ExtraFiles: []*os.File{n.Guard}})
	if err != nil {
		return nil, err
	}
	var result []string
	for _, vm := range vms {
		if vm.Running {
			result = append(result, vm.Name)
		}
	}
	return result, nil
}
func (n Machine) Clone(ctx context.Context, image, vm string) error {
	guard, err := tart.AcquireImageRead(ctx, n.Client.Home, image)
	if err != nil {
		return err
	}
	defer guard.Close()
	exists, _, err := n.LocalVM(ctx, vm)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("tart: refusing to overwrite existing VM %s", vm)
	}
	_, err = n.run(ctx, guard, "clone", image, vm)
	return err
}
func (n Machine) Delete(ctx context.Context, vm string) error {
	exists, running, err := n.LocalVM(ctx, vm)
	if err != nil {
		return err
	}
	if running {
		return fmt.Errorf("tart: cannot delete running VM")
	}
	if !exists {
		return nil
	}
	if _, err = n.run(ctx, nil, "delete", vm); err != nil {
		return err
	}
	exists, _, err = n.LocalVM(ctx, vm)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("tart: deletion was not confirmed")
	}
	return nil
}
