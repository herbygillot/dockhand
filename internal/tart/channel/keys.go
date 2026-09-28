package channel

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/signify"
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

// ArchiveKeys are the keys dockhand signs the archives it gives guests
// with, made on first use (decision 28). MacPorts verifies an archive a
// site serves by whichever of its signature types it asks for first:
// openssl's RIPEMD-160 with an RSA key, the kind pubkeys.conf documents
// for one's own archives, or signify's, the kind MacPorts' own site
// declares. So each archive is signed both ways, as packages.macports.org
// serves both. A guest's MacPorts is told to trust these keys for those
// archives, in a clone that goes when its check is done.
type ArchiveKeys struct {
	Signify signify.Key
	// RSA is the RSA key's file, which openssl signs with, and RSAPublic
	// its public half, as openssl writes one.
	RSA       string
	RSAPublic []byte
}

// ArchiveKeys makes the archive keys once, and reads them after. Two
// processes making them at once keep the first one's.
func (k Keys) ArchiveKeys() (ArchiveKeys, error) {
	var keys ArchiveKeys
	data, err := once(filepath.Join(k.Directory, "archives.key"), func() ([]byte, error) {
		key, err := signify.Generate()
		if err != nil {
			return nil, err
		}
		return key.MarshalBinary()
	})
	if err != nil {
		return keys, err
	}
	if err := keys.Signify.UnmarshalBinary(data); err != nil {
		return keys, err
	}
	keys.RSA = filepath.Join(k.Directory, "archives-rsa.pem")
	data, err = once(keys.RSA, func() ([]byte, error) {
		private, err := rsa.GenerateKey(rand.Reader, 3072)
		if err != nil {
			return nil, err
		}
		return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(private)}), nil
	})
	if err != nil {
		return keys, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return keys, fmt.Errorf("channel: %s is not an RSA key", keys.RSA)
	}
	private, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return keys, fmt.Errorf("channel: %s: %w", keys.RSA, err)
	}
	public, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		return keys, err
	}
	keys.RSAPublic = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})
	return keys, nil
}

// once reads a file, or makes it with what create gives, readable by its
// owner alone. Two processes making it at once keep the first one's.
func once(path string, create func() ([]byte, error)) ([]byte, error) {
	data, err := os.ReadFile(path)
	if !errors.Is(err, os.ErrNotExist) {
		return data, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if data, err = create(); err != nil {
		return nil, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-")
	if err != nil {
		return nil, err
	}
	defer os.Remove(temporary.Name())
	_, err = temporary.Write(data)
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if err := os.Link(temporary.Name(), path); errors.Is(err, os.ErrExist) {
		return os.ReadFile(path)
	} else if err != nil {
		return nil, err
	}
	return data, nil
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
