package provision

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/channel"
	"github.com/herbygillot/dockhand/internal/tart/host"
)

// connectWait bounds how long setup waits for a started guest to accept
// SSH; macOS guests take up to a minute or two to reach sshd.
const connectWait = 4 * time.Minute

// Connect reaches a running guest over Apple's ssh. A bootstrap signs in
// with the image's password, records the host keys the guest presents
// under alias, and installs dockhand's key; every connection after, and a
// connection that is not a bootstrap, uses the key and holds the guest to
// alias's recorded host keys.
func (n *native) Connect(ctx context.Context, name, alias string, bootstrap bool) error {
	address, err := n.vm().IP(ctx, name, 300)
	if err != nil {
		return err
	}
	keys, err := n.keys()
	if err != nil {
		return err
	}
	if bootstrap {
		if err := keys.Forget(alias); err != nil {
			return err
		}
		first := &channel.Guest{Address: address, Image: alias, Keys: keys, Bootstrap: true}
		if err := n.await(ctx, name, first); err != nil {
			return err
		}
		if err := first.InstallKey(ctx); err != nil {
			return err
		}
	}
	guest := &channel.Guest{Address: address, Image: alias, Keys: keys}
	if err := n.await(ctx, name, guest); err != nil {
		return err
	}
	n.mu.Lock()
	n.guests[name] = guest
	n.mu.Unlock()
	return nil
}

// await waits for a guest to accept SSH, reporting the run's own exit if
// the VM stops first. Only a failed connection is waited out; a guest that
// answers and refuses is not.
func (n *native) await(ctx context.Context, name string, guest *channel.Guest) error {
	ctx, cancel := context.WithTimeout(ctx, connectWait)
	defer cancel()
	for {
		output, err := guest.Command(ctx, nil, "/usr/bin/true")
		if err == nil {
			return nil
		}
		if !errors.Is(err, channel.ErrTransport) || strings.Contains(string(output), "Host key verification failed") || strings.Contains(string(output), "Permission denied") {
			return fmt.Errorf("setup: the guest at %s refused SSH: %w", guest.Address, err)
		}
		if run := n.run(name); run != nil {
			select {
			case <-run.Done():
				if run.Err() != nil {
					return run.Err()
				}
				return fmt.Errorf("tart: VM %s stopped before it accepted SSH", name)
			default:
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("setup: the guest at %s did not accept SSH within %s: %w", guest.Address, connectWait, err)
		case <-time.After(2 * time.Second):
		}
	}
}

func (n *native) guestFor(name string) (*channel.Guest, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	guest := n.guests[name]
	if guest == nil {
		return nil, fmt.Errorf("setup: %s has not been reached over SSH", name)
	}
	return guest, nil
}

func (n *native) keys() (channel.Keys, error) {
	if n.sshKeys.Directory != "" {
		return n.sshKeys, nil
	}
	return channel.DefaultKeys()
}

func (n *native) HostKeysRecorded(image string) bool {
	keys, err := n.keys()
	if err != nil {
		return false
	}
	_, err = os.Stat(keys.HostKeys(image))
	return err == nil
}

func (n *native) RecordHostKeys(from, to string) error {
	keys, err := n.keys()
	if err != nil {
		return err
	}
	return keys.Record(from, to)
}

func (n *native) ForgetHostKeys(image string) error {
	keys, err := n.keys()
	if err != nil {
		return err
	}
	return keys.Forget(image)
}

// BootstrapAgent installs the Tart guest agent over SSH. Dockhand no longer
// reads guest output through it, but it grows the guest's disk, and
// verification marks each clone through it.
func (n *native) BootstrapAgent(ctx context.Context, name string) error {
	guest, err := n.guestFor(name)
	if err != nil {
		return err
	}
	if output, err := guest.Script(ctx, nil, agentInstallScript()); err != nil {
		return fmt.Errorf("tart: installing guest agent: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// PersonalImages lists the person's own Tart home, reading nothing else
// there; a home that does not exist has none.
func (n *native) PersonalImages(ctx context.Context) (map[string]image, error) {
	home, err := tart.PersonalHome()
	if err != nil {
		return nil, err
	}
	if home == n.config.Home {
		return map[string]image{}, nil
	}
	if _, err := os.Stat(filepath.Join(home, "vms")); err != nil {
		return map[string]image{}, nil
	}
	values, err := host.Machine{Client: tart.Client{Executable: n.config.Executable, Home: home}}.Images(ctx)
	if err != nil {
		return nil, fmt.Errorf("setup: listing your Tart home %s: %w", home, err)
	}
	result := map[string]image{}
	for _, value := range values {
		if value.Source == "local" {
			result[value.Name] = image{Name: value.Name, Running: value.Running}
		}
	}
	return result, nil
}

// Import copies an image of the person's Tart home into dockhand's with
// Tart's export and import, through a file under ~/.dockhand removed
// afterwards; the person's image is only read.
func (n *native) Import(ctx context.Context, source, destination string) error {
	home, err := tart.PersonalHome()
	if err != nil {
		return err
	}
	user, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	directory := filepath.Join(user, ".dockhand", "imports")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp(directory, source+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	archive := filepath.Join(scratch, source+".tvm")
	theirs := tart.Client{Executable: n.config.Executable, Home: home}
	if err := n.during(ctx, "Exporting "+source, func() error {
		_, err := theirs.Run(ctx, tart.RunOptions{Combined: true}, "export", source, archive)
		return err
	}); err != nil {
		return fmt.Errorf("setup: exporting %s from %s: %w", source, home, err)
	}
	if exists, _, err := n.vm().LocalVM(ctx, destination); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("tart: refusing to overwrite existing VM %s", destination)
	}
	return n.during(ctx, "Importing "+source, func() error {
		_, err := n.command(ctx, nil, false, "import", archive, destination)
		return err
	})
}
