package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tart"
)

type native struct {
	config   Config
	progress io.Writer
	mu       sync.Mutex
	runs     map[string]chan error
}

func newNative(config Config, progress io.Writer) *native {
	return &native{config: config, progress: progress, runs: map[string]chan error{}}
}

func (n *native) command(ctx context.Context, input io.Reader, stream bool, args ...string) ([]byte, error) {
	return n.commandWithGuard(ctx, input, stream, nil, args...)
}

func (n *native) commandWithGuard(ctx context.Context, input io.Reader, stream bool, guard *os.File, args ...string) ([]byte, error) {
	var output io.Writer
	if stream && n.progress != nil {
		output = n.progress
	}
	client := tart.Client{Executable: n.config.Executable, Home: n.config.Home}
	return client.Run(ctx, tart.RunOptions{Input: input, Output: output, Combined: true, ExtraFiles: []*os.File{guard}}, args...)
}

func (n *native) LockSetup(ctx context.Context, image string) (io.Closer, error) {
	return tart.AcquireProvisioning(ctx, n.config.Home, image)
}

func (n *native) Images(ctx context.Context) (map[string]image, error) {
	output, err := n.command(ctx, nil, false, "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	var values []struct {
		Name, Source, State string
		Running             bool
	}
	if err := json.Unmarshal(output, &values); err != nil {
		return nil, fmt.Errorf("tart: invalid image listing: %w", err)
	}
	result := make(map[string]image, len(values))
	for _, value := range values {
		if value.Source == "local" {
			result[value.Name] = image{Name: value.Name, Running: value.Running || value.State == "running"}
		}
	}
	return result, nil
}

func (n *native) Pull(ctx context.Context, source string) error {
	_, err := n.command(ctx, nil, true, "pull", source)
	return err
}

func (n *native) Clone(ctx context.Context, source, destination string) error {
	var guard *os.File
	var err error
	if source == n.config.Image || source == goldenName(n.config.Image) {
		guard, err = tart.AcquireImageRead(ctx, n.config.Home, source)
		if err != nil {
			return err
		}
		defer guard.Close()
	}
	_, err = n.commandWithGuard(ctx, nil, true, guard, "clone", source, destination)
	return err
}

func (n *native) Configure(ctx context.Context, name string) error {
	cpus := max(1, runtime.NumCPU()/4)
	memory := max(8192, cpus*2048)
	if _, err := n.command(ctx, nil, false, "set", name, "--cpu", strconv.Itoa(cpus), "--memory", strconv.Itoa(memory), "--disk-size", "100"); err != nil {
		return err
	}
	if n.config.XcodeArchive == "" {
		return nil
	}
	path := filepath.Join(n.config.Home, "vms", name, "disk.img")
	removed, err := removeRecoveryPartition(path)
	if err != nil {
		return fmt.Errorf("setup: preparing Xcode image storage: %w", err)
	}
	if removed && n.progress != nil {
		_, _ = fmt.Fprintln(n.progress, "Freed the guest recovery partition so Xcode can use the enlarged disk.")
	}
	return nil
}

func (n *native) Start(ctx context.Context, name string) error {
	command := exec.Command(n.config.Executable, "run", "--no-graphics", "--no-audio", "--no-clipboard", name)
	command.Env = append(os.Environ(), "TART_HOME="+n.config.Home, "TART_NO_AUTO_PRUNE=1", "LC_ALL=C")
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	n.mu.Lock()
	n.runs[name] = done
	n.mu.Unlock()
	go func() {
		err := command.Wait()
		if err != nil {
			err = fmt.Errorf("tart: VM %s exited: %w: %s", name, err, strings.TrimSpace(output.String()))
		}
		done <- err
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

func (n *native) runError(name string) <-chan error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.runs[name]
}

func (n *native) guest(ctx context.Context, name string, input io.Reader, args ...string) ([]byte, error) {
	options := []string{"exec"}
	if input != nil {
		options = append(options, "-i")
	}
	options = append(options, name)
	options = append(options, args...)
	return n.command(ctx, input, false, options...)
}

func (n *native) ReadyAgent(ctx context.Context, name string) error {
	for {
		call, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := n.guest(call, name, nil, "/usr/bin/true")
		cancel()
		if err == nil {
			return tart.CheckGuestTransport(ctx, func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
				return n.guest(ctx, name, input, args...)
			})
		}
		if done := n.runError(name); done != nil {
			select {
			case runErr := <-done:
				if runErr == nil {
					runErr = fmt.Errorf("tart: VM %s stopped before its guest agent became ready", name)
				}
				return runErr
			default:
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (n *native) Stop(ctx context.Context, name string) error {
	images, err := n.Images(ctx)
	if err != nil {
		return err
	}
	if current := images[name]; current.Name != "" && current.Running {
		if _, err := n.command(ctx, nil, false, "stop", name); err != nil {
			return err
		}
	}
	if done := n.runError(name); done != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		case <-time.After(10 * time.Second):
			return fmt.Errorf("tart: timed out waiting for VM %s to stop", name)
		}
	}
	return nil
}

func (n *native) Delete(ctx context.Context, name string) error {
	images, err := n.Images(ctx)
	if err != nil {
		return err
	}
	if images[name].Name == "" {
		return nil
	}
	if images[name].Running {
		return fmt.Errorf("tart: refusing to delete running image %s", name)
	}
	_, err = n.command(ctx, nil, false, "delete", name)
	return err
}

func (n *native) Rename(ctx context.Context, from, to string) error {
	_, err := n.command(ctx, nil, false, "rename", from, to)
	return err
}

func (n *native) Adopt(ctx context.Context, source, destination string, replace bool) error {
	guard, err := tart.AcquireImageWrite(ctx, n.config.Home, destination)
	if err != nil {
		return err
	}
	defer guard.Close()
	if replace {
		if err := n.Delete(ctx, destination); err != nil {
			return err
		}
	}
	_, err = n.commandWithGuard(ctx, nil, true, guard, "clone", source, destination)
	return err
}

func (n *native) BootstrapAgent(ctx context.Context, name string) error {
	output, err := n.command(ctx, nil, false, "ip", name, "--wait", "300")
	if err != nil {
		return err
	}
	host := strings.TrimSpace(string(output))
	if host == "" {
		return fmt.Errorf("tart: VM %s has no IP address", name)
	}
	if err := waitSSH(ctx, host); err != nil {
		return err
	}
	outputText, err := sshRun(ctx, host, agentInstallScript())
	if err != nil {
		return fmt.Errorf("tart: installing guest agent: %w: %s", err, strings.TrimSpace(outputText))
	}
	return nil
}

func (n *native) target(name string) macos.Command {
	return func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
		return n.guest(ctx, name, input, args...)
	}
}
func (n *native) streamTarget(name string) macos.Command {
	return func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
		return n.guestStream(ctx, name, input, args...)
	}
}
func (n *native) EnsureToolchain(ctx context.Context, name string) error {
	return macos.EnsureCommandLineTools(ctx, n.target(name))
}

func (n *native) InstallXcode(ctx context.Context, name string, config Config) error {
	if err := macos.EnsureAPFSSpace(ctx, n.target(name), "/private/tmp", "disk0", "disk0s2", macos.XcodeExpansionSpaceGiB); err != nil {
		return fmt.Errorf("setup: expanding the Xcode image filesystem: %w", err)
	}
	output, err := n.command(ctx, nil, false, "ip", name, "--wait", "300")
	if err != nil {
		return err
	}
	host := strings.TrimSpace(string(output))
	if host == "" {
		return fmt.Errorf("tart: VM %s has no IP address", name)
	}
	if err := waitSSH(ctx, host); err != nil {
		return err
	}
	info, err := os.Stat(config.XcodeArchive)
	if err != nil {
		return err
	}
	if n.progress != nil {
		_, _ = fmt.Fprintf(n.progress, "Copying Xcode %s into the guest (%.1f GiB)...\n", config.XcodeVersion, float64(info.Size())/(1<<30))
	}
	const guestArchive = "/private/tmp/Xcode.xip"
	if err := sshPush(ctx, host, config.XcodeArchive, guestArchive); err != nil {
		return fmt.Errorf("setup: copying Xcode archive: %w", err)
	}
	if n.progress != nil {
		_, _ = fmt.Fprintf(n.progress, "Expanding and installing Xcode %s...\n", config.XcodeVersion)
	}
	return macos.InstallXcode(ctx, n.streamTarget(name), guestArchive)
}

func installerName(version string, release macos.Release) string {
	return fmt.Sprintf("MacPorts-%s-%s-%s.pkg", version, release.Product, strings.ReplaceAll(release.Name, " ", ""))
}

func (n *native) InstallMacPorts(ctx context.Context, name string, config Config, release macos.Release) error {
	filename := installerName(config.MacPortsVersion, release)
	url := "https://distfiles.macports.org/MacPorts/" + filename
	script := `set -eu
file="/tmp/$1"
/usr/bin/curl -fsSL -o "$file" "$2"
sudo -n /usr/sbin/installer -pkg "$file" -target /
/bin/rm -f "$file"`
	_, err := n.guest(ctx, name, nil, "/bin/sh", "-c", script, "dockhand", filename, url)
	return err
}

func (n *native) WriteManifest(ctx context.Context, name string, manifest []byte) error {
	if _, err := n.guest(ctx, name, nil, "sudo", "-n", "/bin/mkdir", "-p", "/opt/dockhand"); err != nil {
		return err
	}
	_, err := n.guest(ctx, name, bytes.NewReader(append(manifest, '\n')), "sudo", "-n", "/usr/bin/tee", "/opt/dockhand/image.json")
	return err
}

func (n *native) Validate(ctx context.Context, name string, config Config) (validation, error) {
	if _, err := n.guest(ctx, name, nil, "sudo", "-n", "/usr/bin/true"); err != nil {
		return validation{}, fmt.Errorf("passwordless sudo is unavailable: %w", err)
	}
	foreign := `set -eu
for path in /opt/homebrew /usr/local/Homebrew /usr/local/Cellar /sw /opt/pkg /etc/paths.d/homebrew /etc/paths.d/fink; do
  [ ! -e "$path" ] || { echo "$path"; exit 1; }
done`
	if output, err := n.guest(ctx, name, nil, "/bin/sh", "-c", foreign); err != nil {
		return validation{}, fmt.Errorf("foreign package manager found: %s: %w", strings.TrimSpace(string(output)), err)
	}
	port := filepath.Join(config.GuestPrefix, "bin", "port")
	installed, err := n.guest(ctx, name, nil, port, "-q", "installed", "active")
	if err != nil {
		return validation{}, err
	}
	if strings.TrimSpace(string(installed)) != "" {
		return validation{}, fmt.Errorf("prepared image has active ports: %s", strings.TrimSpace(string(installed)))
	}
	versionOutput, err := n.guest(ctx, name, nil, port, "version")
	if err != nil {
		return validation{}, err
	}
	fields := strings.Fields(string(versionOutput))
	if len(fields) < 2 || fields[0] != "Version:" {
		return validation{}, fmt.Errorf("MacPorts returned an unrecognized version: %s", strings.TrimSpace(string(versionOutput)))
	}
	tcl := `printf '%s\n' 'package require json' 'package require json::write' 'package require macports' 'mportinit' 'puts "$::macports::os_platform $::macports::os_major $::macports::build_arch"' | "$1/bin/port-tclsh"`
	platformOutput, err := n.guest(ctx, name, nil, "/bin/sh", "-c", tcl, "dockhand", config.GuestPrefix)
	if err != nil {
		return validation{}, err
	}
	platformFields := strings.Fields(string(platformOutput))
	if len(platformFields) != 3 {
		return validation{}, fmt.Errorf("MacPorts returned an unrecognized platform: %s", strings.TrimSpace(string(platformOutput)))
	}
	if err := macos.CheckCompiler(ctx, n.target(name)); err != nil {
		return validation{}, err
	}
	xcodeVersion, err := n.validateXcode(ctx, name, config)
	if err != nil {
		return validation{}, err
	}
	agentVersion, err := tart.ObserveGuestAgentVersion(ctx, func(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
		return n.guest(ctx, name, input, args...)
	})
	if err != nil {
		return validation{}, err
	}
	return validation{Platform: record.Platform{OS: platformFields[0], Version: platformFields[1], Architecture: platformFields[2]}, MacPortsVersion: fields[1], GuestAgentVersion: agentVersion, XcodeVersion: xcodeVersion}, nil
}

func (n *native) guestStream(ctx context.Context, name string, input io.Reader, args ...string) ([]byte, error) {
	options := []string{"exec"}
	if input != nil {
		options = append(options, "-i")
	}
	options = append(options, name)
	options = append(options, args...)
	return n.command(ctx, input, true, options...)
}

func (n *native) validateXcode(ctx context.Context, name string, config Config) (string, error) {
	tools, err := macos.InspectDeveloperTools(ctx, n.target(name))
	if err != nil {
		return "", err
	}
	if len(tools.Problems) != 0 {
		return "", fmt.Errorf("developer tools: %s", strings.Join(tools.Problems, "; "))
	}
	if config.XcodeVersion == "" {
		if !tools.CommandLineTools() {
			return "", fmt.Errorf("base image selects unexpected developer directory %s", tools.Directory)
		}
		return "", nil
	}
	if tools.Directory != "/Applications/Xcode.app/Contents/Developer" {
		return "", fmt.Errorf("Xcode image selects unexpected developer directory %s", tools.Directory)
	}
	if tools.XcodeVersion != config.XcodeVersion {
		return "", fmt.Errorf("image has Xcode %s; expected %s", tools.XcodeVersion, config.XcodeVersion)
	}
	return tools.XcodeVersion, nil
}
