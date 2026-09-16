package tart

import (
	"context"
	"encoding/json"
	"github.com/herbygillot/dockhand/internal/atomicfile"
	"io"
	"os"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/state"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/host"
)

const guestDirectory = "/var/tmp/dockhand2"
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
	_, err = n.guest(ctx, vm, file, "sudo", "-n", "/bin/sh", "-c", `set -eu
[ ! -e /var/tmp/dockhand2 ]
mkdir -m 755 /var/tmp/dockhand2
exec /usr/bin/tar xf - -C /var/tmp/dockhand2`)
	return err
}
func (n *native) Launch(ctx context.Context, vm string) error {
	_, err := n.guest(ctx, vm, nil, "sudo", "-n", "/bin/sh", "-c", `set -eu
if /bin/launchctl print system/org.dockhand2.build >/dev/null 2>&1; then exit 0; fi
[ ! -f /var/tmp/dockhand2/result.json ] || exit 0
exec /bin/launchctl bootstrap system /var/tmp/dockhand2/guest.plist`)
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
	out, err := n.guest(ctx, vm, nil, "sudo", "-n", "/bin/sh", "-c", `set -eu
if [ -f /var/tmp/dockhand2/result.json ]; then exec cat /var/tmp/dockhand2/result.json; fi
if status=$(/bin/launchctl print system/org.dockhand2.build 2>&1); then
  case "$status" in
    *'state = not running'*)
      if [ -f /var/tmp/dockhand2/result.json ]; then cat /var/tmp/dockhand2/result.json
      else echo '{"State":"runner-exited"}'; fi ;;
    *) echo '{"State":"starting"}' ;;
  esac
else
  echo '{"State":"not-started"}'
fi`)
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
		_, err := n.execGuest(ctx, vm, nil, file, "sudo", "-n", "/bin/sh", "-c", "if [ -f /var/tmp/dockhand2/build.log ]; then cat /var/tmp/dockhand2/build.log; fi; cat /var/tmp/dockhand2/runner.log")
		return err
	})
}
