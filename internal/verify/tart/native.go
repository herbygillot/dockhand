package tart

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/atomicfile"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/state"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/channel"
	"github.com/herbygillot/dockhand/internal/tart/host"
)

const guestDirectory = "/var/tmp/dockhand2"

// guestShell fills the guest directory into a shell script written with
// @guest@ where the directory goes, so the constant is the one place the
// path is spelled.
func guestShell(script string) string { return strings.ReplaceAll(script, "@guest@", guestDirectory) }

const guestLabel = "org.dockhand2.build"

type native struct {
	host.Machine
	config Config
	images *imageCache
	cache  state.ImageCache
	// keys is dockhand's SSH material; empty selects ~/.dockhand/ssh.
	keys channel.Keys
	// ssh replaces Apple's client in tests.
	ssh string
}

func newNative(config Config, guard *os.File, images *imageCache, cache state.ImageCache) *native {
	return &native{Machine: host.Machine{Client: tartvm.Client{Executable: config.Executable, Home: config.Home}, Guard: guard}, config: config, images: images, cache: cache}
}

// personalBlocked stands for the person's VMs when a running one with an
// ASIF disk keeps their Tart home from listing (openai/tart#1344): at least
// that one is running.
const personalBlocked = "personal: a running VM with an ASIF disk"

// Running is every running VM that counts toward the Mac's limit of two
// running macOS VMs: dockhand's own, and the person's, from a listing of
// their Tart home that takes and changes nothing there. A personal
// listing a running ASIF VM blocks counts as that one VM; a personal home
// that does not exist counts none.
func (n *native) Running(ctx context.Context) ([]string, error) {
	own, err := n.Machine.Running(ctx)
	if err != nil {
		return nil, err
	}
	personal, err := tartvm.PersonalHome()
	if err != nil || personal == n.Client.Home {
		return own, nil
	}
	if _, err := os.Stat(filepath.Join(personal, "vms")); err != nil {
		return own, nil
	}
	theirs := host.Machine{Client: tartvm.Client{Executable: n.Client.Executable, Home: personal}}
	names, err := theirs.Running(ctx)
	switch {
	case errors.Is(err, tartvm.ErrListingBlocked):
		return append(own, personalBlocked), nil
	case err != nil:
		return nil, fmt.Errorf("tart: counting the VMs running in %s: %w", personal, err)
	}
	for _, name := range names {
		own = append(own, "personal:"+name)
	}
	return own, nil
}

// reach is the guest of a running clone over SSH, held to the host keys
// recorded for the image it was cloned from; its address is Tart's.
func (n *native) reach(ctx context.Context, vm string) (*channel.Guest, error) {
	address, err := n.IP(ctx, vm, 60)
	if err != nil {
		return nil, err
	}
	keys := n.keys
	if keys.Directory == "" {
		if keys, err = channel.DefaultKeys(); err != nil {
			return nil, err
		}
	}
	return &channel.Guest{Address: address, Image: n.config.Image, Keys: keys, Executable: n.ssh}, nil
}

// guest runs a command in the clone over SSH.
func (n *native) guest(ctx context.Context, vm string, input io.Reader, args ...string) ([]byte, error) {
	guest, err := n.reach(ctx, vm)
	if err != nil {
		return nil, err
	}
	return guest.Command(ctx, input, args...)
}

// errVMLimit is a clone Tart would not start because the Mac already runs
// its limit of two macOS VMs, some started outside what dockhand counts.
var errVMLimit = errors.New("tart: the Mac already runs its limit of two macOS VMs")

// identityFile is where Ready marks a clone through Tart's own channel to
// it, so the guest SSH reaches can be told to be that clone.
const identityFile = "/var/tmp/dockhand-identity"

// Ready waits for a started clone: its guest agent answering, the clone
// marked with a token through `tart exec`, which reaches exactly the named
// VM, and that token read back over SSH, which proves the address Tart
// gave is the clone's and not an earlier VM's lease. A clone that stops
// first says why, from its run's log in directory; one refused at the
// Mac's VM limit is errVMLimit.
func (n *native) Ready(ctx context.Context, vm, directory string) error {
	for attempt := 0; ; attempt++ {
		call, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := n.Exec(call, vm, tartvm.RunOptions{}, "/usr/bin/true")
		cancel()
		if err == nil {
			break
		}
		if attempt%5 == 4 {
			if _, running, listErr := n.LocalVM(ctx, vm); listErr == nil && !running {
				log, _ := os.ReadFile(filepath.Join(directory, "vm.log"))
				detail := strings.TrimSpace(string(log))
				if strings.Contains(detail, "exceeds the system limit") {
					return fmt.Errorf("%w: %s", errVMLimit, detail)
				}
				return fmt.Errorf("tart: VM %s stopped before its guest agent answered: %s", vm, detail)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	token := rand.Text()
	if _, err := n.Exec(ctx, vm, tartvm.RunOptions{Input: strings.NewReader(token)}, "/bin/sh", "-c", `umask 022; cat > `+identityFile); err != nil {
		return fmt.Errorf("tart: marking %s: %w", vm, err)
	}
	guest, err := n.reach(ctx, vm)
	if err != nil {
		return err
	}
	wait, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	for {
		marked, err := guest.Read(wait, identityFile, false)
		if err == nil {
			if string(marked) != token {
				return fmt.Errorf("tart: the guest at %s is not %s", guest.Address, vm)
			}
			return host.CheckGuestTransport(ctx, guest.Command)
		}
		if !errors.Is(err, channel.ErrTransport) {
			return fmt.Errorf("tart: reading %s's mark over SSH: %w", vm, err)
		}
		select {
		case <-wait.Done():
			return fmt.Errorf("tart: %s did not accept SSH at %s: %w", vm, guest.Address, err)
		case <-time.After(2 * time.Second):
		}
	}
}

func guestPlist(prefix string) []byte {
	return macos.LaunchdPlist(guestLabel, []string{prefix + "/bin/port-tclsh", guestDirectory + "/guest.tcl"}, guestDirectory+"/runner.log", map[string]string{"PATH": prefix + "/bin:" + prefix + "/sbin:/usr/bin:/bin:/usr/sbin:/sbin"})
}

// Stage copies the prepared input into the clone, checked by size and
// sha256, and unpacks it into the guest directory.
func (n *native) Stage(ctx context.Context, vm, archive string) error {
	guest, err := n.reach(ctx, vm)
	if err != nil {
		return err
	}
	const input = "/var/tmp/dockhand2-input.tar"
	if err := guest.Upload(ctx, archive, input, true); err != nil {
		return err
	}
	_, err = guest.Command(ctx, nil, "sudo", "-n", "/bin/sh", "-c", guestShell(`set -eu
[ ! -e @guest@ ]
mkdir -m 755 @guest@
/usr/bin/tar xf "$1" -C @guest@
rm -f "$1"`), "dockhand", input)
	return err
}

func (n *native) Launch(ctx context.Context, vm string) error {
	_, err := n.guest(ctx, vm, nil, "sudo", "-n", "/bin/sh", "-c", guestShell(`set -eu
if /bin/launchctl print system/org.dockhand2.build >/dev/null 2>&1; then exit 0; fi
[ ! -f @guest@/result.json ] || exit 0
exec /bin/launchctl bootstrap system @guest@/guest.plist`))
	return err
}

// Inspect reports the build's state. result.json, the verdict, is read as
// a checked transfer; the runner's state is a word from launchctl.
func (n *native) Inspect(ctx context.Context, vm string) (guestResult, error) {
	exists, running, err := n.LocalVM(ctx, vm)
	if err != nil {
		return guestResult{}, err
	}
	if !exists || !running {
		return guestResult{State: "stopped"}, nil
	}
	guest, err := n.reach(ctx, vm)
	if err != nil {
		return guestResult{}, err
	}
	readResult := func() (guestResult, error) {
		var result guestResult
		data, err := guest.Read(ctx, guestDirectory+"/result.json", true)
		if err != nil {
			return result, err
		}
		return result, json.Unmarshal(data, &result)
	}
	out, err := guest.Command(ctx, nil, "sudo", "-n", "/bin/sh", "-c", guestShell(`set -eu
if [ -f @guest@/result.json ]; then echo result; exit 0; fi
if status=$(/bin/launchctl print system/org.dockhand2.build 2>&1); then
  case "$status" in
    *'state = not running'*)
      if [ -f @guest@/result.json ]; then echo result; else echo runner-exited; fi ;;
    *) echo starting ;;
  esac
else
  echo not-started
fi`))
	if err != nil {
		return guestResult{}, err
	}
	state := strings.TrimSpace(string(out))
	if state != "result" {
		return guestResult{State: state}, nil
	}
	result, err := readResult()
	if err != nil {
		return guestResult{}, err
	}
	if result.State == "running" {
		status, e := guest.Command(ctx, nil, "sudo", "-n", "/bin/launchctl", "print", "system/"+guestLabel)
		if e != nil {
			return guestResult{}, e
		}
		if strings.Contains(string(status), "state = not running") {
			// The runner may have published its terminal result after our first read.
			if result, err = readResult(); err != nil {
				return guestResult{}, err
			}
			if result.State == "running" {
				return guestResult{State: "runner-exited"}, nil
			}
		}
	}
	return result, nil
}

// Logs copies the build log, when there is one, and the runner's log out
// of the clone, each checked by size and sha256, into one file.
func (n *native) Logs(ctx context.Context, vm, path string) error {
	guest, err := n.reach(ctx, vm)
	if err != nil {
		return err
	}
	scratch, err := os.MkdirTemp(filepath.Dir(path), ".logs-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	var parts []string
	for _, name := range []string{"build.log", "runner.log"} {
		local := filepath.Join(scratch, name)
		err := guest.Download(ctx, guestDirectory+"/"+name, local, true)
		if errors.Is(err, os.ErrNotExist) && name == "build.log" {
			continue
		}
		if err != nil {
			return err
		}
		parts = append(parts, local)
	}
	return atomicfile.Create(path, 0600, func(file *os.File) error {
		for _, part := range parts {
			data, err := os.Open(part)
			if err != nil {
				return err
			}
			_, err = io.Copy(file, data)
			if err := errors.Join(err, data.Close()); err != nil {
				return err
			}
		}
		return nil
	})
}
