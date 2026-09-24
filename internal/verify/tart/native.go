package tart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/atomicfile"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/state"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
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

func (n *native) guest(ctx context.Context, vm string, input io.Reader, args ...string) ([]byte, error) {
	return n.Exec(ctx, vm, tartvm.RunOptions{Input: input}, args...)
}
func (n *native) execGuest(ctx context.Context, vm string, input io.Reader, output io.Writer, args ...string) ([]byte, error) {
	return n.Exec(ctx, vm, tartvm.RunOptions{Input: input, Output: output}, args...)
}
func guestPlist(prefix string) []byte {
	return macos.LaunchdPlist(guestLabel, []string{prefix + "/bin/port-tclsh", guestDirectory + "/guest.tcl"}, guestDirectory+"/runner.log", map[string]string{"PATH": prefix + "/bin:" + prefix + "/sbin:/usr/bin:/bin:/usr/sbin:/sbin"})
}
func (n *native) Stage(ctx context.Context, vm, archive string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = n.guest(ctx, vm, file, "sudo", "-n", "/bin/sh", "-c", guestShell(`set -eu
[ ! -e @guest@ ]
mkdir -m 755 @guest@
exec /usr/bin/tar xf - -C @guest@`))
	return err
}
func (n *native) Launch(ctx context.Context, vm string) error {
	_, err := n.guest(ctx, vm, nil, "sudo", "-n", "/bin/sh", "-c", guestShell(`set -eu
if /bin/launchctl print system/org.dockhand2.build >/dev/null 2>&1; then exit 0; fi
[ ! -f @guest@/result.json ] || exit 0
exec /bin/launchctl bootstrap system @guest@/guest.plist`))
	return err
}
func (n *native) Inspect(ctx context.Context, vm string) (guestResult, error) {
	exists, running, err := n.LocalVM(ctx, vm)
	if err != nil {
		return guestResult{}, err
	}
	if !exists || !running {
		return guestResult{State: "stopped"}, nil
	}
	out, err := n.guest(ctx, vm, nil, "sudo", "-n", "/bin/sh", "-c", guestShell(`set -eu
if [ -f @guest@/result.json ]; then exec cat @guest@/result.json; fi
if status=$(/bin/launchctl print system/org.dockhand2.build 2>&1); then
  case "$status" in
    *'state = not running'*)
      if [ -f @guest@/result.json ]; then cat @guest@/result.json
      else echo '{"State":"runner-exited"}'; fi ;;
    *) echo '{"State":"starting"}' ;;
  esac
else
  echo '{"State":"not-started"}'
fi`))
	if err != nil {
		return guestResult{}, err
	}
	var result guestResult
	err = json.Unmarshal(out, &result)
	if err != nil {
		return result, err
	}
	if result.State == "running" {
		status, e := n.guest(ctx, vm, nil, "sudo", "-n", "/bin/launchctl", "print", "system/"+guestLabel)
		if e != nil {
			return guestResult{}, e
		}
		if strings.Contains(string(status), "state = not running") {
			// The runner may have published its terminal result after our first read.
			final, err := n.guest(ctx, vm, nil, "sudo", "-n", "/bin/cat", guestDirectory+"/result.json")
			if err != nil {
				return guestResult{}, err
			}
			if err := json.Unmarshal(final, &result); err != nil {
				return guestResult{}, err
			}
			if result.State == "running" {
				return guestResult{State: "runner-exited"}, nil
			}
		}
	}
	return result, nil
}
func (n *native) Logs(ctx context.Context, vm, path string) error {
	return atomicfile.Create(path, 0600, func(file *os.File) error {
		_, err := n.execGuest(ctx, vm, nil, file, "sudo", "-n", "/bin/sh", "-c", guestShell("if [ -f @guest@/build.log ]; then cat @guest@/build.log; fi; cat @guest@/runner.log"))
		return err
	})
}
