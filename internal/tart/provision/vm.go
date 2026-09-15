package provision

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/tart"
)

func (n *native) LockSetup(ctx context.Context, image string) (io.Closer, error) {
	return tart.AcquireProvisioning(ctx, n.config.Home, image)
}

func (n *native) Images(ctx context.Context) (map[string]image, error) {
	values, err := (tart.Client{Executable: n.config.Executable, Home: n.config.Home}).Images(ctx, tart.RunOptions{})
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
	_, err = n.commandWithGuard(ctx, nil, true, guard, "clone", source, destination)
	return err
}

func (n *native) Configure(ctx context.Context, name string) error {
	cpus := max(1, runtime.NumCPU()/4)
	memory := max(8192, cpus*2048)
	if _, err := n.command(ctx, nil, false, "set", name, "--cpu", strconv.Itoa(cpus), "--memory", strconv.Itoa(memory), "--disk-size", "100"); err != nil {
		return err
	}
	if n.config.XcodeArchive == "" {
		return nil
	}
	path := filepath.Join(n.config.Home, "vms", name, "disk.img")
	removed, err := macos.RemoveRecoveryPartition(path)
	if err != nil {
		return fmt.Errorf("setup: preparing Xcode image storage: %w", err)
	}
	if removed && n.progress != nil {
		_, _ = fmt.Fprintln(n.progress, "Freed the guest recovery partition so Xcode can use the enlarged disk.")
	}
	return nil
}

func (n *native) Start(ctx context.Context, name string) error {
	done, err := n.vm().StartForeground(name)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.runs[name] = done
	n.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

func (n *native) runError(name string) <-chan error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.runs[name]
}

func (n *native) Stop(ctx context.Context, name string) error {
	images, err := n.Images(ctx)
	if err != nil {
		return err
	}
	if current := images[name]; current.Name != "" && current.Running {
		if _, err := n.command(ctx, nil, false, "stop", name); err != nil {
			return err
		}
	}
	if done := n.runError(name); done != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		case <-time.After(10 * time.Second):
			return fmt.Errorf("tart: timed out waiting for VM %s to stop", name)
		}
	}
	return nil
}

func (n *native) Delete(ctx context.Context, name string) error {
	return n.vm().Delete(ctx, name)
}

func (n *native) Rename(ctx context.Context, from, to string) error {
	_, err := n.command(ctx, nil, false, "rename", from, to)
	return err
}

func (n *native) Adopt(ctx context.Context, source, destination string, replace bool) error {
	guard, err := tart.AcquireImageWrite(ctx, n.config.Home, destination)
	if err != nil {
		return err
	}
	defer guard.Close()
	return adopt(ctx, adoptionMachine{n, guard}, source, destination, replace)
}
