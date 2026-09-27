package tart

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/channel"
	"github.com/herbygillot/dockhand/internal/tart/host"
)

// machine is the Mac's VMs as the provider uses them; tests replace it.
type machine interface {
	// Images are the VMs in dockhand's Tart home, running or not.
	Images(ctx context.Context) ([]string, error)
	// Running counts the VMs running on the Mac, the person's among them:
	// the Mac runs two macOS VMs at most, whoever started them.
	Running(ctx context.Context) (int, error)
	Clone(ctx context.Context, image, vm string) error
	// Start runs a VM owned by this process.
	Start(vm string) (run, error)
	// Stop stops a VM by name, one an earlier process started.
	Stop(ctx context.Context, vm string) error
	Delete(ctx context.Context, vm string) error
	// Cached are the images Tart pulled into dockhand's Tart home, and
	// DeleteCached removes one by name.
	Cached(ctx context.Context) ([]tartvm.Image, error)
	DeleteCached(ctx context.Context, name string) error
	// Reach is the guest of a running clone over SSH, held to the host keys
	// recorded for the image it was cloned from.
	Reach(ctx context.Context, vm, image string) (guest, error)
}

// run is a VM this process started.
type run interface {
	Done() <-chan struct{}
	Err() error
	Stop(ctx context.Context, grace time.Duration) error
}

// guest is a running clone, reached over dockhand's SSH channel.
type guest interface {
	Command(ctx context.Context, input io.Reader, args ...string) ([]byte, error)
	Upload(ctx context.Context, local, path string, sudo bool) error
	Read(ctx context.Context, path string, sudo bool) ([]byte, error)
	Download(ctx context.Context, path, local string, sudo bool) error
	Close(ctx context.Context)
}

// native is the Mac's Tart, in dockhand's own home.
type native struct {
	host.Machine
	keys channel.Keys
}

func newNative(client tartvm.Client, keys channel.Keys) *native {
	return &native{Machine: host.Machine{Client: client}, keys: keys}
}

func (n *native) Images(ctx context.Context) ([]string, error) {
	images, err := n.Machine.Images(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, image := range images {
		names = append(names, image.Name)
	}
	return names, nil
}

func (n *native) Cached(ctx context.Context) ([]tartvm.Image, error) {
	images, err := n.Machine.Images(ctx)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(images, func(image tartvm.Image) bool { return image.Source != "OCI" }), nil
}

// Running counts dockhand's running VMs and the person's, from a listing
// of their Tart home that takes and changes nothing there. A personal
// listing a running ASIF VM blocks counts as that one VM.
func (n *native) Running(ctx context.Context) (int, error) {
	own, err := n.Machine.Running(ctx)
	if err != nil {
		return 0, err
	}
	count := len(own)
	personal, err := tartvm.PersonalHome()
	if err != nil || personal == n.Client.Home {
		return count, nil
	}
	if _, err := os.Stat(filepath.Join(personal, "vms")); err != nil {
		return count, nil
	}
	theirs := host.Machine{Client: tartvm.Client{Executable: n.Client.Executable, Home: personal}}
	names, err := theirs.Running(ctx)
	switch {
	case errors.Is(err, tartvm.ErrListingBlocked):
		return count + 1, nil
	case err != nil:
		return 0, fmt.Errorf("counting the VMs running in %s: %w", personal, err)
	}
	return count + len(names), nil
}

func (n *native) Start(vm string) (run, error) {
	started, err := n.StartForeground(vm)
	if err != nil {
		return nil, err
	}
	return started, nil
}

// Stop stops a VM an earlier process started and left running.
func (n *native) Stop(ctx context.Context, vm string) error {
	_, err := n.Client.Run(ctx, tartvm.RunOptions{}, "stop", "--timeout", "30", vm)
	if errors.Is(err, tartvm.ErrVMStopped) || errors.Is(err, tartvm.ErrVMMissing) {
		return nil
	}
	return err
}

func (n *native) Reach(ctx context.Context, vm, image string) (guest, error) {
	address, err := n.IP(ctx, vm, 300)
	if err != nil {
		return nil, err
	}
	return &channel.Guest{Address: address, Image: image, Keys: n.keys}, nil
}

// await waits for a booted guest to accept SSH; macOS guests take a minute
// or two to reach sshd. A run that stops first ends the wait.
func await(ctx context.Context, g guest, vm run, wait time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		_, err := g.Command(ctx, nil, "/usr/bin/true")
		if err == nil {
			return nil
		}
		if !errors.Is(err, channel.ErrTransport) {
			return err
		}
		select {
		case <-vm.Done():
			return fmt.Errorf("the VM stopped before it accepted SSH: %v", vm.Err())
		case <-ctx.Done():
			return fmt.Errorf("the guest never accepted SSH: %w", err)
		case <-time.After(3 * time.Second):
		}
	}
}

// vmName is the clone an attempt builds in: the run's, the release's, and
// the attempt's, so a later attempt can find and remove what an earlier
// one left when its process died.
func vmName(prefix string, attempt int) string {
	return fmt.Sprintf("%s-%d", prefix, attempt)
}

// clonePrefixAll begins the name of every check's clone, and of nothing
// else in dockhand's Tart home: images are dockhand-base-, dockhand-xcode-,
// and dockhand-golden-.
const clonePrefixAll = "dockhand-check-"

// clonePrefix names every attempt's clone for one run and release.
func clonePrefix(run, release string) string {
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, run)
	return clonePrefixAll + safe + "-" + release
}
