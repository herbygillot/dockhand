// Package channel reaches a Tart guest over SSH through Apple's
// /usr/bin/ssh, macOS's own documented client, which Local Network privacy
// does not stop the way it stops a Go program's direct dial, whichever app
// launched dockhand (decision 43 of the contracts direction). Commands
// share one multiplexed connection per guest. Files move by `ssh … cat`,
// every transfer checked against a size and sha256 computed on the other
// side, since a transport can report success while losing data.
package channel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/subprocess"
)

// User is the account Cirrus Labs' macOS images log in as.
const User = "admin"

// Password is the account's password in those images. Setup uses it once,
// to install dockhand's key; everything after uses the key.
const Password = "admin"

// SSH is Apple's client.
const SSH = "/usr/bin/ssh"

// ErrTransport is a failure of the connection rather than of the command:
// ssh's own exit status, 255.
var ErrTransport = errors.New("channel: the SSH connection failed")

// ErrTransfer is a transfer whose size or sha256 on arrival differs from
// the other side's.
var ErrTransfer = errors.New("channel: transfer arrived damaged")

// Guest is one guest reached at an address, trusted as the image whose host
// keys dockhand recorded.
type Guest struct {
	Address string
	// Image names the host keys the guest must present: every clone of an
	// image shares them, so they are recorded by image, not by address.
	Image string
	Keys  Keys
	// Bootstrap authenticates with the image's password rather than the
	// key and records the host keys the guest presents as Image's: setup's
	// first contact with a guest dockhand has not reached before.
	Bootstrap bool
	// Executable is the ssh client, SSH unless a test replaces it.
	Executable string
	// Output, when set, receives a command's output as it arrives.
	Output io.Writer
}

// Command runs one command, its arguments quoted for the guest's shell,
// and returns what it wrote to stdout and stderr together. A command that
// fails returns its output and an error carrying its exit status; a
// connection that fails returns ErrTransport.
func (g *Guest) Command(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
	return g.run(ctx, input, g.Output, true, Quote(args...))
}

// Script runs a shell script on the guest, with arguments $1 and on.
func (g *Guest) Script(ctx context.Context, input io.Reader, script string, args ...string) ([]byte, error) {
	return g.Command(ctx, input, append([]string{"/bin/sh", "-c", script, "dockhand"}, args...)...)
}

func (g *Guest) run(ctx context.Context, input io.Reader, stream io.Writer, combined bool, command string) ([]byte, error) {
	arguments, environment, cleanup, err := g.invocation()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "ssh", Command: "guest", Path: g.executable(), Args: append(arguments, g.Address, command), Env: environment, Stdin: input, Stdout: stream, Combined: combined})
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 255 {
			// The exit status is ssh's, not the command's, so it is not
			// passed on: a caller reading exit statuses as the command's
			// verdict would take a lost connection for a failed check.
			return result.Output, fmt.Errorf("%w: %s: %v", ErrTransport, g.Address, err)
		}
		return result.Output, err
	}
	return result.Output, nil
}

func (g *Guest) executable() string {
	if g.Executable != "" {
		return g.Executable
	}
	return SSH
}

// invocation is ssh's options: none of the person's own configuration, the
// image's recorded host keys under the image's name, and either dockhand's
// key over a shared connection or, at bootstrap, the password through
// SSH_ASKPASS with the presented host keys recorded.
func (g *Guest) invocation() (args, environment []string, cleanup func(), err error) {
	cleanup = func() {}
	if g.Address == "" || g.Image == "" || g.Keys.Directory == "" {
		return nil, nil, cleanup, fmt.Errorf("channel: a guest needs an address, an image, and dockhand's keys")
	}
	hosts := g.Keys.HostKeys(g.Image)
	if err := os.MkdirAll(filepath.Dir(hosts), 0700); err != nil {
		return nil, nil, cleanup, err
	}
	args = []string{
		"-F", "/dev/null",
		"-o", "User=" + User,
		"-o", "UserKnownHostsFile=" + hosts,
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "HostKeyAlias=" + g.Image,
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=4",
		"-o", "LogLevel=ERROR",
	}
	environment = os.Environ()
	if g.Bootstrap {
		askpass, err := os.MkdirTemp("", "dockhand-askpass-")
		if err != nil {
			return nil, nil, cleanup, err
		}
		cleanup = func() { os.RemoveAll(askpass) }
		program := filepath.Join(askpass, "askpass")
		if err := os.WriteFile(program, []byte("#!/bin/sh\necho "+Password+"\n"), 0700); err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
		args = append(args,
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "PubkeyAuthentication=no",
			"-o", "PreferredAuthentications=keyboard-interactive,password",
			"-o", "NumberOfPasswordPrompts=1",
			"-o", "ControlMaster=no",
		)
		environment = append(environment, "SSH_ASKPASS="+program, "SSH_ASKPASS_REQUIRE=force", "DISPLAY=dockhand")
		return args, environment, cleanup, nil
	}
	control, err := controlPath(g.Address, g.Image)
	if err != nil {
		return nil, nil, cleanup, err
	}
	args = append(args,
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "IdentityFile="+g.Keys.Private(),
		"-o", "IdentitiesOnly=yes",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath="+control,
		"-o", "ControlPersist=120",
	)
	return args, environment, cleanup, nil
}

// controlPath is the shared connection's socket for one guest: under /tmp,
// since a socket's path is limited to 104 bytes and the per-user temporary
// directory's is long, and named for the address and image so every
// dockhand process reaching the same guest shares one connection.
func controlPath(address, image string) (string, error) {
	directory := filepath.Join("/tmp", "dockhand-ssh-"+strconv.Itoa(os.Getuid()))
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 {
		return "", fmt.Errorf("channel: %s must be a private directory", directory)
	}
	return filepath.Join(directory, record.Digest([]byte(address + "\x00" + image))[:16]), nil
}

// Close ends the shared connection to the guest, if one is open.
func (g *Guest) Close(ctx context.Context) {
	if g.Bootstrap {
		return
	}
	control, err := controlPath(g.Address, g.Image)
	if err != nil {
		return
	}
	if _, err := os.Stat(control); err != nil {
		return
	}
	_, _ = subprocess.Run(ctx, subprocess.Spec{Tool: "ssh", Command: "close", Path: g.executable(), Args: []string{"-F", "/dev/null", "-o", "ControlPath=" + control, "-O", "exit", g.Address}})
}

// Quote joins arguments into one command line for a POSIX shell, each
// argument single-quoted.
func Quote(args ...string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}
