package provision

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/host"
)

func (n *native) LockSetup(ctx context.Context, image string) (io.Closer, error) {
	return tart.AcquireProvisioning(ctx, n.config.Home, image)
}

func (n *native) Images(ctx context.Context) (map[string]image, error) {
	values, err := n.vm().Images(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]image, len(values))
	for _, value := range values {
		if value.Source == "local" {
			result[value.Name] = image{Name: value.Name, Running: value.Running}
		}
	}
	return result, nil
}

func (n *native) Pull(ctx context.Context, source string) error {
	_, err := n.command(ctx, nil, true, "pull", source)
	return err
}

// Clone refuses a destination Tart already lists, as host.Machine.Clone
// does: `tart clone` onto a stopped VM's name replaces it silently.
func (n *native) Clone(ctx context.Context, source, destination string) error {
	var guard *os.File
	var err error
	if source == n.config.Image || source == goldenName(n.config.Image) {
		guard, err = tart.AcquireImageRead(ctx, n.config.Home, source)
		if err != nil {
			return err
		}
		defer guard.Close()
	}
	if exists, _, err := n.vm().LocalVM(ctx, destination); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("tart: refusing to overwrite existing VM %s", destination)
	}
	_, err = n.commandWithGuard(ctx, nil, true, guard, "clone", source, destination)
	return err
}

func (n *native) DiskFormat(ctx context.Context, name string) (string, error) {
	return n.vm().DiskFormat(ctx, name)
}

// Configure sizes a raw-disk guest and frees its recovery partition, so the
// guest agent can grow the container over the whole 100 GB disk. Editing
// disk.img on the host is a flagged exception to using only what Tart
// documents (decision 38): it is the route Tart's FAQ points to, Cirrus's
// Packer plugin, takes the same way, and it runs only here, on setup's own
// freshly cloned, stopped VM, whose format `tart get` has said is raw.
func (n *native) Configure(ctx context.Context, name string) error {
	cpus := max(1, runtime.NumCPU()/4)
	memory := max(8192, cpus*2048)
	if _, err := n.command(ctx, nil, false, "set", name, "--cpu", strconv.Itoa(cpus), "--memory", strconv.Itoa(memory), "--disk-size", "100"); err != nil {
		return err
	}
	format, err := n.DiskFormat(ctx, name)
	if err != nil {
		return err
	}
	if format != "raw" {
		return fmt.Errorf("setup: %s has a %s disk; its storage is prepared only on a raw disk", name, format)
	}
	path := filepath.Join(n.config.Home, "vms", name, "disk.img")
	removed, err := macos.RemoveRecoveryPartition(path)
	if err != nil {
		return fmt.Errorf("setup: preparing image storage: %w", err)
	}
	if removed && n.progress != nil {
		_, _ = fmt.Fprintln(n.progress, "Freed the guest recovery partition so the guest can use the enlarged disk.")
	}
	return nil
}

func (n *native) Start(ctx context.Context, name string) error {
	run, err := n.vm().StartForeground(name)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.runs[name] = run
	n.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-run.Done():
		return run.Err()
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

func (n *native) run(name string) *host.Foreground {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.runs[name]
}

// Stop stops a guest this setup started through its kept `tart run`: `tart
// stop`, then SIGINT, then SIGKILL. It never asks the listing first, which a
// running ASIF VM can keep from answering (openai/tart#1344). A VM setup did
// not start is stopped with `tart stop` alone, "not running" included.
func (n *native) Stop(ctx context.Context, name string) error {
	if run := n.run(name); run != nil {
		return run.Stop(ctx, 10*time.Second)
	}
	_, err := n.command(ctx, nil, false, "stop", name)
	if errors.Is(err, tart.ErrVMStopped) || errors.Is(err, tart.ErrVMMissing) {
		return nil
	}
	return err
}

func (n *native) Delete(ctx context.Context, name string) error {
	return n.vm().Delete(ctx, name)
}

func (n *native) Rename(ctx context.Context, from, to string) error {
	return n.vm().Rename(ctx, from, to)
}

func (n *native) Adopt(ctx context.Context, source, destination string, replace bool) error {
	guard, err := tart.AcquireImageWrite(ctx, n.config.Home, destination)
	if err != nil {
		return err
	}
	defer guard.Close()
	return adopt(ctx, adoptionMachine{n, guard}, source, destination, replace)
}
