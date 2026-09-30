package channel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/subprocess"
)

// Keys is dockhand's SSH material in one directory: its key pair, which
// setup installs in every image, and the host keys recorded for each
// image, which verification holds its guests to.
type Keys struct {
	Directory string
}

// DefaultKeys is ~/.dockhand/ssh, or DOCKHAND_SSH_DIR.
func DefaultKeys() (Keys, error) {
	if directory := os.Getenv("DOCKHAND_SSH_DIR"); directory != "" {
		return Keys{Directory: directory}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Keys{}, err
	}
	return Keys{Directory: filepath.Join(home, ".dockhand", "ssh")}, nil
}

// Private is the private key's path.
func (k Keys) Private() string { return filepath.Join(k.Directory, "id_ed25519") }

// HostKeys is the file of host keys recorded for an image.
func (k Keys) HostKeys(image string) string {
	return filepath.Join(k.Directory, "hosts", strings.NewReplacer("/", "_", ":", "_").Replace(image))
}

// Public creates the key pair with Apple's ssh-keygen when there is none,
// and returns the public key's line for authorized_keys.
func (k Keys) Public(ctx context.Context) (string, error) {
	if err := os.MkdirAll(k.Directory, 0700); err != nil {
		return "", err
	}
	if _, err := os.Stat(k.Private()); errors.Is(err, os.ErrNotExist) {
		if _, err := subprocess.Run(ctx, subprocess.Spec{Tool: "ssh-keygen", Path: "/usr/bin/ssh-keygen", Args: []string{"-q", "-t", "ed25519", "-N", "", "-C", "dockhand", "-f", k.Private()}}); err != nil {
			return "", fmt.Errorf("channel: creating dockhand's SSH key: %w", err)
		}
	} else if err != nil {
		return "", err
	}
	public, err := os.ReadFile(k.Private() + ".pub")
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(public))
	if !strings.HasPrefix(line, "ssh-ed25519 ") {
		return "", fmt.Errorf("channel: %s.pub is not an ed25519 public key", k.Private())
	}
	return line, nil
}

// Forget removes an image's recorded host keys, before setup records the
// ones its new guest presents.
func (k Keys) Forget(image string) error {
	err := os.Remove(k.HostKeys(image))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Record copies the host keys recorded under one image to another, as
// setup does when a proven candidate becomes the image. The entries name
// the source image, so they are rewritten to name the destination.
func (k Keys) Record(from, to string) error {
	data, err := os.ReadFile(k.HostKeys(from))
	if err != nil {
		return err
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		fields[0] = to
		lines = append(lines, strings.Join(fields, " "))
	}
	if len(lines) == 0 {
		return fmt.Errorf("channel: no host keys are recorded for %s", from)
	}
	destination := k.HostKeys(to)
	temporary := destination + ".next"
	if err := os.WriteFile(temporary, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return err
	}
	return os.Rename(temporary, destination)
}

// InstallKey authorizes dockhand's public key for the guest's account over
// a bootstrap connection, so every later connection uses the key.
func (g *Guest) InstallKey(ctx context.Context) error {
	public, err := g.Keys.Public(ctx)
	if err != nil {
		return err
	}
	_, err = g.Script(ctx, strings.NewReader(public+"\n"), `set -eu
umask 077
mkdir -p "$HOME/.ssh"
key=$(cat)
touch "$HOME/.ssh/authorized_keys"
grep -qxF "$key" "$HOME/.ssh/authorized_keys" || printf '%s\n' "$key" >> "$HOME/.ssh/authorized_keys"
chmod 700 "$HOME/.ssh"
chmod 600 "$HOME/.ssh/authorized_keys"`)
	if err != nil {
		return fmt.Errorf("channel: installing dockhand's key: %w", err)
	}
	return nil
}
