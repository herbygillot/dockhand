package host

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/tart"
)

// Machine carries the runtime and optional operation lock inherited by commands.
type Machine struct {
	Client tart.Client
	Guard  *os.File
	// Blocked, when set, is called each time the listing is blocked by a
	// running ASIF VM (tart.ErrListingBlocked); it waits and returns nil to
	// list again, or returns an error to give up. Without it the listing's
	// error is returned, and the caller waits as it waits for capacity.
	Blocked func(context.Context) error
}

func (n Machine) list(ctx context.Context) ([]tart.Image, error) {
	for {
		vms, err := n.Client.Images(ctx, tart.RunOptions{ExtraFiles: []*os.File{n.Guard}})
		if !errors.Is(err, tart.ErrListingBlocked) || n.Blocked == nil {
			return vms, err
		}
		if err := n.Blocked(ctx); err != nil {
			return nil, err
		}
	}
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

// LocalVM reports a local VM from Tart's listing, the one account of what
// exists that Tart documents. A clone Tart killed midway leaves nothing, or
// a temporary directory Tart's next command removes, so nothing on disk is
// consulted.
func (n Machine) LocalVM(ctx context.Context, name string) (exists, running bool, err error) {
	vms, err := n.list(ctx)
	if err != nil {
		return false, false, err
	}
	for _, vm := range vms {
		if vm.Name == name && vm.Source == "local" {
			return true, vm.Running, nil
		}
	}
	return false, false, nil
}

// Images lists every VM and image Tart has.
func (n Machine) Images(ctx context.Context) ([]tart.Image, error) { return n.list(ctx) }

// IP is a running VM's address from `tart ip`, waiting up to wait seconds
// for the VM to take one.
func (n Machine) IP(ctx context.Context, vm string, wait int) (string, error) {
	out, err := n.run(ctx, nil, "ip", vm, "--wait", strconv.Itoa(wait))
	if err != nil {
		return "", err
	}
	address := strings.TrimSpace(string(out))
	if net.ParseIP(address) == nil {
		return "", fmt.Errorf("tart: VM %s has no IP address: %q", vm, address)
	}
	return address, nil
}

// DiskFormat reports a VM's disk format, "raw" or "asif", from `tart get`.
func (n Machine) DiskFormat(ctx context.Context, name string) (string, error) {
	vm, err := n.Client.Get(ctx, tart.RunOptions{ExtraFiles: []*os.File{n.Guard}}, name)
	return vm.DiskFormat, err
}

func (n Machine) Running(ctx context.Context) ([]string, error) {
	vms, err := n.list(ctx)
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

// Clone refuses a destination Tart already lists: `tart clone` onto a
// stopped VM's name replaces that VM without a word.
func (n Machine) Clone(ctx context.Context, image, vm string) error {
	guard, err := tart.AcquireImageRead(ctx, n.Client.Home, image)
	if err != nil {
		return err
	}
	defer guard.Close()
	if err := n.absent(ctx, vm); err != nil {
		return err
	}
	_, err = n.run(ctx, guard, "clone", image, vm)
	return err
}

// Rename moves a stopped VM to a name Tart does not list. Tart renames a
// running VM too, which would move it out from under whoever runs it.
func (n Machine) Rename(ctx context.Context, from, to string) error {
	exists, running, err := n.LocalVM(ctx, from)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("tart: cannot rename %s: %w", from, tart.ErrVMMissing)
	}
	if running {
		return fmt.Errorf("tart: cannot rename running VM %s", from)
	}
	if err := n.absent(ctx, to); err != nil {
		return err
	}
	_, err = n.run(ctx, nil, "rename", from, to)
	return err
}

func (n Machine) absent(ctx context.Context, vm string) error {
	exists, _, err := n.LocalVM(ctx, vm)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("tart: refusing to overwrite existing VM %s", vm)
	}
	return nil
}

// Delete removes a VM the listing shows stopped and confirms the removal by
// its absence from the listing. What `tart delete` says is not evidence:
// it reports a running VM as one that "does not exist" and leaves it
// (openai/tart#1345).
func (n Machine) Delete(ctx context.Context, vm string) error {
	exists, running, err := n.LocalVM(ctx, vm)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if running {
		return fmt.Errorf("tart: cannot delete running VM %s", vm)
	}
	_, deleted := n.run(ctx, nil, "delete", vm)
	exists, _, err = n.LocalVM(ctx, vm)
	if err != nil {
		return errors.Join(deleted, err)
	}
	if exists {
		if deleted != nil {
			return fmt.Errorf("tart: VM %s is still listed after delete: %w", vm, deleted)
		}
		return fmt.Errorf("tart: VM %s is still listed after delete", vm)
	}
	return nil
}
