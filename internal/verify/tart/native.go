package tart

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/state"
)

const guestDirectory = "/var/tmp/dockhand2"
const guestLabel = "org.dockhand2.build"

const guestExecScript = `set -eu
limit=$(ulimit -S -n)
ulimit -S -n "$(ulimit -H -n)"
for fd in /dev/fd/*; do
    fd=${fd##*/}
    if [ "$fd" -gt 2 ]; then eval "exec $fd>&-"; fi
done
ulimit -S -n "$limit"
exec "$@"
`

type native struct {
	config Config
	guard  *os.File
	images *imageCache
	cache  state.ImageCache
}

func (n *native) command(ctx context.Context, bin string, input io.Reader, output io.Writer, args ...string) ([]byte, error) {
	return n.commandWithGuard(ctx, bin, input, output, nil, args...)
}

func (n *native) commandWithGuard(ctx context.Context, bin string, input io.Reader, output io.Writer, imageGuard *os.File, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "TART_HOME="+n.config.Home, "TART_NO_AUTO_PRUNE=1", "LC_ALL=C")
	cmd.Stdin = input
	cmd.WaitDelay = 2 * time.Second
	// Close-only locks remain held by unfinished commands after driver death.
	if n.guard != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, n.guard)
	}
	if imageGuard != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, imageGuard)
	}
	var out, stderr bytes.Buffer
	if output == nil {
		cmd.Stdout = &out
	} else {
		cmd.Stdout = output
	}
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return out.Bytes(), fmt.Errorf("tart: %s: %w: %s", filepath.Base(bin), errors.Join(ctx.Err(), err), strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}
func (n *native) tart(ctx context.Context, input io.Reader, output io.Writer, args ...string) ([]byte, error) {
	return n.command(ctx, n.config.Executable, input, output, args...)
}
func (n *native) guest(ctx context.Context, vm string, input io.Reader, args ...string) ([]byte, error) {
	return n.execGuest(ctx, vm, input, nil, args...)
}

func (n *native) execGuest(ctx context.Context, vm string, input io.Reader, output io.Writer, args ...string) ([]byte, error) {
	options := []string{"exec"}
	if input != nil {
		options = append(options, "-i")
	}
	options = append(options, vm, "/bin/sh", "-c", guestExecScript, "dockhand")
	options = append(options, args...)
	return n.tart(ctx, input, output, options...)
}
func (n *native) localVM(ctx context.Context, name string) (exists, running bool, err error) {
	out, err := n.tart(ctx, nil, nil, "list", "--format", "json")
	if err != nil {
		return false, false, err
	}
	var vms []struct {
		Name, Source, State string
		Running             bool
	}
	if err = json.Unmarshal(out, &vms); err != nil {
		return false, false, err
	}
	for _, vm := range vms {
		if vm.Name == name && vm.Source == "local" {
			return true, vm.Running || vm.State == "running", nil
		}
	}
	// An incomplete clone is an owned resource even if Tart cannot list it.
	_, err = os.Lstat(filepath.Join(n.config.Home, "vms", name))
	if err == nil {
		return true, false, fmt.Errorf("tart: VM %s exists but could not be listed", name)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, false, err
	}
	return false, false, nil
}
func (n *native) Running(ctx context.Context) ([]string, error) {
	out, err := n.tart(ctx, nil, nil, "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	var vms []struct {
		Name, State string
		Running     bool
	}
	if err = json.Unmarshal(out, &vms); err != nil {
		return nil, err
	}
	var result []string
	for _, vm := range vms {
		if vm.Running || vm.State == "running" {
			result = append(result, vm.Name)
		}
	}
	return result, nil
}
func (n *native) Clone(ctx context.Context, image, vm string) error {
	guard, err := AcquireImageRead(ctx, n.config.Home, image)
	if err != nil {
		return err
	}
	defer guard.Close()
	exists, _, err := n.localVM(ctx, vm)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("tart: refusing to overwrite existing VM %s", vm)
	}
	_, err = n.commandWithGuard(ctx, n.config.Executable, nil, nil, guard, "clone", image, vm)
	return err
}
func xmlString(s string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(s))
	return out.String()
}
func plist(label string, args []string, log string, env map[string]string) []byte {
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + xmlString(label) + `</string><key>ProgramArguments</key><array>`)
	for _, arg := range args {
		out.WriteString("<string>" + xmlString(arg) + "</string>")
	}
	out.WriteString(`</array><key>RunAtLoad</key><true/><key>KeepAlive</key><false/><key>AbandonProcessGroup</key><false/><key>StandardOutPath</key><string>` + xmlString(log) + `</string><key>StandardErrorPath</key><string>` + xmlString(log) + `</string><key>EnvironmentVariables</key><dict>`)
	for key, value := range env {
		out.WriteString("<key>" + xmlString(key) + "</key><string>" + xmlString(value) + "</string>")
	}
	out.WriteString("</dict></dict></plist>")
	return []byte(out.String())
}
func guestPlist(prefix string) []byte {
	return plist(guestLabel, []string{prefix + "/bin/port-tclsh", guestDirectory + "/guest.tcl"}, guestDirectory+"/runner.log", map[string]string{"PATH": prefix + "/bin:" + prefix + "/sbin:/usr/bin:/bin:/usr/sbin:/sbin"})
}
func hostLabel(vm string) string { return "org.dockhand2.vm." + vm }
func hostDomain() string         { return "gui/" + strconv.Itoa(os.Getuid()) }
func (n *native) service(ctx context.Context, target string) (bool, error) {
	_, err := n.command(ctx, "/bin/launchctl", nil, nil, "print", target)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 113 && strings.Contains(err.Error(), "Could not find service") {
		return false, nil
	}
	return false, err
}
func (n *native) Start(ctx context.Context, vm, directory string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("tart: launchd requires macOS")
	}
	bin, err := exec.LookPath(n.config.Executable)
	if err != nil {
		return err
	}
	path := filepath.Join(directory, "vm.plist")
	data := plist(hostLabel(vm), []string{bin, "run", "--no-graphics", "--no-audio", "--no-clipboard", vm}, filepath.Join(directory, "vm.log"), map[string]string{"TART_HOME": n.config.Home, "TART_NO_AUTO_PRUNE": "1"})
	if err = atomicFile(path, data, 0600); err != nil {
		return err
	}
	loaded, err := n.service(ctx, hostDomain()+"/"+hostLabel(vm))
	if err != nil {
		return err
	}
	if loaded {
		return nil
	}
	_, err = n.command(ctx, "/bin/launchctl", nil, nil, "bootstrap", hostDomain(), path)
	return err
}
func (n *native) Ready(ctx context.Context, vm string) error {
	for {
		call, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := n.guest(call, vm, nil, "/usr/bin/true")
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
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
	exists, running, err := n.localVM(ctx, vm)
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
	file, err := os.CreateTemp(filepath.Dir(path), ".log-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = n.execGuest(ctx, vm, nil, file, "sudo", "-n", "/bin/sh", "-c", "if [ -f /var/tmp/dockhand2/build.log ]; then cat /var/tmp/dockhand2/build.log; fi; cat /var/tmp/dockhand2/runner.log")
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}
func (n *native) Stop(ctx context.Context, vm string) error {
	target := hostDomain() + "/" + hostLabel(vm)
	loaded, err := n.service(ctx, target)
	if err != nil {
		return err
	}
	if loaded {
		_, err = n.command(ctx, "/bin/launchctl", nil, nil, "bootout", "--wait", target)
		if err != nil {
			return err
		}
	}
	for {
		loaded, err = n.service(ctx, target)
		if err != nil {
			return err
		}
		_, running, err := n.localVM(ctx, vm)
		if err != nil {
			return err
		}
		if !loaded && !running {
			return nil
		}
		if running {
			if _, err = n.tart(ctx, nil, nil, "stop", vm); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
func (n *native) Delete(ctx context.Context, vm string) error {
	exists, running, err := n.localVM(ctx, vm)
	if err != nil {
		return err
	}
	if running {
		return fmt.Errorf("tart: cannot delete running VM")
	}
	if !exists {
		return nil
	}
	if _, err = n.tart(ctx, nil, nil, "delete", vm); err != nil {
		return err
	}
	exists, _, err = n.localVM(ctx, vm)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("tart: deletion was not confirmed")
	}
	return nil
}
