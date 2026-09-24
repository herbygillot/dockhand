package channel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/subprocess"
)

// attempts is how often a damaged transfer is tried before it is reported.
const attempts = 3

// missing is the exit status the transfer scripts give a file that is not
// there, so it is reported as os.ErrNotExist rather than as a failure.
const missing = 3

// digestScript prints a file's size and sha256, with sudo when asked; the
// file must be a regular file.
const digestScript = `set -eu
[ -f "$1" ] || exit 3
/usr/bin/stat -f %z "$1"
/usr/bin/openssl dgst -sha256 -r "$1" | /usr/bin/cut -d' ' -f1`

// Upload copies a local file to path on the guest, with sudo when asked,
// and checks what arrived by its size and sha256 on the guest.
func (g *Guest) Upload(ctx context.Context, local, path string, sudo bool) error {
	size, digest, err := fileDigest(local)
	if err != nil {
		return err
	}
	for attempt := 1; ; attempt++ {
		file, err := os.Open(local)
		if err != nil {
			return err
		}
		_, err = g.run(ctx, file, nil, true, Quote(elevate(sudo, "/bin/sh", "-c", `umask 077; cat > "$1"`, "dockhand", path)...))
		file.Close()
		if err != nil {
			return fmt.Errorf("channel: sending %s: %w", path, err)
		}
		gotSize, gotDigest, err := g.digest(ctx, path, sudo)
		if err != nil {
			return err
		}
		if gotSize == size && gotDigest == digest {
			return nil
		}
		if attempt == attempts {
			return fmt.Errorf("%w: %s arrived as %d bytes with sha256 %s, sent %d bytes with sha256 %s", ErrTransfer, path, gotSize, gotDigest, size, digest)
		}
	}
}

// Download copies path on the guest to a local file, with sudo when asked,
// and checks what arrived against its size and sha256 on the guest. A file
// that is not there is os.ErrNotExist.
func (g *Guest) Download(ctx context.Context, path, local string, sudo bool) error {
	for attempt := 1; ; attempt++ {
		size, digest, err := g.digest(ctx, path, sudo)
		if err != nil {
			return err
		}
		temporary, err := os.CreateTemp(filepath.Dir(local), "."+filepath.Base(local)+".")
		if err != nil {
			return err
		}
		hash := sha256.New()
		counted := &counter{}
		err = g.stream(ctx, io.MultiWriter(temporary, hash, counted), elevate(sudo, "/bin/cat", path)...)
		if closeErr := temporary.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(temporary.Name())
			return fmt.Errorf("channel: reading %s: %w", path, err)
		}
		if counted.n == size && hex.EncodeToString(hash.Sum(nil)) == digest {
			return os.Rename(temporary.Name(), local)
		}
		os.Remove(temporary.Name())
		if attempt == attempts {
			return fmt.Errorf("%w: %s arrived as %d bytes, the guest has %d", ErrTransfer, path, counted.n, size)
		}
	}
}

// Read returns a small file from the guest, checked like Download.
func (g *Guest) Read(ctx context.Context, path string, sudo bool) ([]byte, error) {
	for attempt := 1; ; attempt++ {
		size, digest, err := g.digest(ctx, path, sudo)
		if err != nil {
			return nil, err
		}
		var data bytes.Buffer
		if err := g.stream(ctx, &data, elevate(sudo, "/bin/cat", path)...); err != nil {
			return nil, fmt.Errorf("channel: reading %s: %w", path, err)
		}
		sum := sha256.Sum256(data.Bytes())
		if int64(data.Len()) == size && hex.EncodeToString(sum[:]) == digest {
			return data.Bytes(), nil
		}
		if attempt == attempts {
			return nil, fmt.Errorf("%w: %s arrived as %d bytes, the guest has %d", ErrTransfer, path, data.Len(), size)
		}
	}
}

// Range reads up to limit bytes of a file that may still be growing, from
// offset, and checks them against the sha256 of the same bytes read again
// on the guest: bytes already written do not change as a log grows. A
// file that is not there is os.ErrNotExist.
func (g *Guest) Range(ctx context.Context, path string, offset int64, limit int, sudo bool) ([]byte, error) {
	if offset < 0 || limit <= 0 {
		return nil, fmt.Errorf("channel: invalid range")
	}
	start := strconv.FormatInt(offset+1, 10)
	for attempt := 1; ; attempt++ {
		var data bytes.Buffer
		err := g.stream(ctx, &data, elevate(sudo, "/bin/sh", "-c", `[ -f "$1" ] || exit 3; /usr/bin/tail -c +"$2" "$1" | /usr/bin/head -c "$3"`, "dockhand", path, start, strconv.Itoa(limit))...)
		if err != nil {
			return nil, err
		}
		if data.Len() == 0 {
			return nil, nil
		}
		output, err := g.run(ctx, nil, nil, false, Quote(elevate(sudo, "/bin/sh", "-c", `/usr/bin/tail -c +"$2" "$1" | /usr/bin/head -c "$3" | /usr/bin/openssl dgst -sha256 -r | /usr/bin/cut -d' ' -f1`, "dockhand", path, start, strconv.Itoa(data.Len()))...))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data.Bytes())
		if strings.TrimSpace(string(output)) == hex.EncodeToString(sum[:]) {
			return data.Bytes(), nil
		}
		if attempt == attempts {
			return nil, fmt.Errorf("%w: %s from byte %d", ErrTransfer, path, offset)
		}
	}
}

// stream runs a command whose standard output is data, sent to w alone, and
// reports a file the command found missing as os.ErrNotExist.
func (g *Guest) stream(ctx context.Context, w io.Writer, args ...string) error {
	arguments, environment, cleanup, err := g.invocation()
	if err != nil {
		return err
	}
	defer cleanup()
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "ssh", Command: "guest", Path: g.executable(), Args: append(arguments, g.Address, Quote(args...)), Env: environment, Stdout: w, StdoutOnly: true, Limit: 1 << 20})
	return classify(g.Address, result, err)
}

func (g *Guest) digest(ctx context.Context, path string, sudo bool) (int64, string, error) {
	arguments, environment, cleanup, err := g.invocation()
	if err != nil {
		return 0, "", err
	}
	defer cleanup()
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "ssh", Command: "guest", Path: g.executable(), Args: append(arguments, g.Address, Quote(elevate(sudo, "/bin/sh", "-c", digestScript, "dockhand", path)...)), Env: environment, Limit: 1 << 20})
	if err := classify(g.Address, result, err); err != nil {
		return 0, "", err
	}
	fields := strings.Fields(string(result.Output))
	if len(fields) != 2 {
		return 0, "", fmt.Errorf("channel: unreadable size and sha256 of %s: %q", path, result.Output)
	}
	size, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || len(fields[1]) != 64 {
		return 0, "", fmt.Errorf("channel: unreadable size and sha256 of %s: %q", path, result.Output)
	}
	return size, fields[1], nil
}

func classify(address string, result subprocess.Result, err error) error {
	if err == nil {
		return nil
	}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case missing:
			return fmt.Errorf("%w: %w", os.ErrNotExist, err)
		case 255:
			return fmt.Errorf("%w: %s: %v", ErrTransport, address, err)
		}
	}
	return err
}

// elevate prefixes a command with sudo when asked; the image's account may
// use sudo without a password, which setup checks.
func elevate(sudo bool, args ...string) []string {
	if sudo {
		return append([]string{"/usr/bin/sudo", "-n"}, args...)
	}
	return args
}

func fileDigest(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

type counter struct{ n int64 }

func (c *counter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}
