package macos

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// XcodeExpansionSpaceGiB is the free space required before staging an Xcode archive.
const XcodeExpansionSpaceGiB = 60

func CheckCompiler(ctx context.Context, run Command) error {
	// Quiet: on a fresh guest this is expected to fail, and a streamed
	// "xcode-select: error" would read as setup's own failure.
	if _, err := run(ctx, nil, "/bin/sh", "-c", "/usr/bin/xcode-select -p >/dev/null 2>&1"); err != nil {
		return fmt.Errorf("guest has no selected command line tools: %w", err)
	}
	script := `set -eu
file=$(/usr/bin/mktemp /tmp/dockhand-setup-compiler.XXXXXX)
trap 'rm -f "$file"' EXIT
printf 'int main(void) { return 0; }\n' | /usr/bin/clang -x c - -o "$file"
"$file"
rm -f "$file"`
	_, err := run(ctx, nil, "/bin/sh", "-c", script)
	return err
}

// commandLineToolsMarker makes softwareupdate offer the Command Line Tools,
// as the tools' own installer does.
const commandLineToolsMarker = "/tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress"

// EnsureCommandLineTools installs the newest Command Line Tools of the
// release's generation that Software Update offers (decision 13), never the
// newest of all: sorting the offers once gave the Tahoe images the macOS 27
// tools. It refuses, naming what is offered, when no tools of the
// generation are, and refuses tools already installed of another
// generation.
func EnsureCommandLineTools(ctx context.Context, run Command, release Release) error {
	if release.Tools == 0 {
		return fmt.Errorf("macos: no Command Line Tools generation is recorded for %s", release.Name)
	}
	if err := CheckCompiler(ctx, run); err == nil {
		return checkToolsGeneration(ctx, run, release)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	list := `set -eu
sudo -n /usr/bin/touch "$1"
labels=''
attempt=1
while [ "$attempt" -le 6 ]; do
  labels=$(/usr/sbin/softwareupdate --list 2>/dev/null | /usr/bin/sed -n 's/^\* Label: \(.*Command Line Tools.*\)$/\1/p')
  [ -n "$labels" ] && break
  /bin/sleep 15
  attempt=$((attempt + 1))
done
printf '%s\n' "$labels"`
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, _ = run(cleanup, nil, "sudo", "-n", "/bin/rm", "-f", commandLineToolsMarker)
	}()
	output, err := run(ctx, nil, "/bin/sh", "-c", list, "dockhand", commandLineToolsMarker)
	if err != nil {
		return fmt.Errorf("guest command line tools listing failed: %w", err)
	}
	var labels []string
	for _, line := range strings.Split(string(output), "\n") {
		if line = strings.TrimSpace(line); strings.Contains(line, "Command Line Tools") {
			labels = append(labels, line)
		}
	}
	label, err := CommandLineToolsLabel(labels, release)
	if err != nil {
		return err
	}
	install := `set -eu
printf 'Installing %s...\n' "$1"
sudo -n /usr/sbin/softwareupdate --install "$1"`
	if _, err := run(ctx, nil, "/bin/sh", "-c", install, "dockhand", label); err != nil {
		return fmt.Errorf("guest command line tools installation failed: %w", err)
	}
	if err := CheckCompiler(ctx, run); err != nil {
		return err
	}
	return checkToolsGeneration(ctx, run, release)
}

// CommandLineToolsLabel chooses, among the labels Software Update offers,
// the newest Command Line Tools of the release's generation.
func CommandLineToolsLabel(labels []string, release Release) (string, error) {
	var chosen string
	var newest []int
	for _, label := range labels {
		version, ok := commandLineToolsVersion(label)
		if !ok || version[0] != release.Tools {
			continue
		}
		if chosen == "" || compareVersions(version, newest) > 0 {
			chosen, newest = label, version
		}
	}
	if chosen == "" {
		offered := "none"
		if len(labels) > 0 {
			offered = strings.Join(labels, ", ")
		}
		return "", fmt.Errorf("macos: Software Update offers no Command Line Tools %d, the generation %s uses; offered: %s", release.Tools, release.Name, offered)
	}
	return chosen, nil
}

// commandLineToolsVersion reads the tools version from a Software Update
// label: "Command Line Tools for Xcode 27.0-27.0" is 27.0, and "Command
// Line Tools for Xcode-14.2" is 14.2.
func commandLineToolsVersion(label string) ([]int, bool) {
	_, rest, ok := strings.Cut(label, "for Xcode")
	if !ok {
		return nil, false
	}
	rest = strings.TrimLeft(rest, " -")
	rest, _, _ = strings.Cut(rest, "-")
	return parseVersion(strings.TrimSpace(rest))
}

func parseVersion(text string) ([]int, bool) {
	var version []int
	for _, part := range strings.Split(text, ".") {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, false
		}
		version = append(version, number)
	}
	return version, len(version) > 0
}

func compareVersions(a, b []int) int {
	for i := range max(len(a), len(b)) {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

// CommandLineToolsVersion reports the installed Command Line Tools'
// version, such as 26.6, from its package receipt, or "" when none is
// installed.
func CommandLineToolsVersion(ctx context.Context, run Command) (string, error) {
	output, err := run(ctx, nil, "/bin/sh", "-c", "/usr/sbin/pkgutil --pkg-info=com.apple.pkg.CLTools_Executables 2>/dev/null || true")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(output), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "version: "); ok {
			parts := strings.Split(value, ".")
			if len(parts) < 2 {
				return "", fmt.Errorf("macos: unrecognized Command Line Tools version %q", value)
			}
			return parts[0] + "." + parts[1], nil
		}
	}
	return "", nil
}

// checkToolsGeneration refuses Command Line Tools of another generation
// than the release's.
func checkToolsGeneration(ctx context.Context, run Command, release Release) error {
	version, err := CommandLineToolsVersion(ctx, run)
	if err != nil {
		return err
	}
	if version == "" {
		return fmt.Errorf("guest has a compiler but no Command Line Tools package; setup installs Command Line Tools %d for %s", release.Tools, release.Name)
	}
	if parsed, ok := parseVersion(version); !ok || parsed[0] != release.Tools {
		return fmt.Errorf("guest has Command Line Tools %s; %s uses generation %d", version, release.Name, release.Tools)
	}
	return nil
}

// InstallXcode expands an archive already present on the command target and selects the installation.
func InstallXcode(ctx context.Context, run Command, archive string) error {
	if !filepath.IsAbs(archive) {
		return fmt.Errorf("macos: Xcode archive path must be absolute")
	}
	install := `set -eu
archive=$1
work=$(/usr/bin/mktemp -d /private/tmp/dockhand-xcode.XXXXXX)
trap 'status=$?; if [ "$status" -eq 0 ]; then /bin/rm -rf "$work"; else printf "Xcode installation failed; workspace retained at %s\n" "$work" >&2; fi' EXIT
/bin/mv "$archive" "$work/Xcode.xip"
cd "$work"
printf 'Expanding Xcode archive...\n'
/usr/bin/xip --expand Xcode.xip
/bin/rm -f Xcode.xip
sudo -n /bin/rm -rf /Applications/Xcode.app
sudo -n /bin/mv Xcode.app /Applications/Xcode.app
sudo -n /usr/bin/xcode-select -s /Applications/Xcode.app/Contents/Developer
printf 'Configuring Xcode and installing first-launch components...\n'
sudo -n /usr/bin/xcodebuild -license accept
sudo -n /usr/bin/xcodebuild -runFirstLaunch`
	if output, err := run(ctx, nil, "/bin/sh", "-c", install, "dockhand", archive); err != nil {
		return fmt.Errorf("macos: installing Xcode: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
