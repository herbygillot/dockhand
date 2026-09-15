package macos

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// XcodeExpansionSpaceGiB is the free space required before staging an Xcode archive.
const XcodeExpansionSpaceGiB = 60

func CheckCompiler(ctx context.Context, run Command) error {
	if _, err := run(ctx, nil, "/usr/bin/xcode-select", "-p"); err != nil {
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

func EnsureCommandLineTools(ctx context.Context, run Command) error {
	if err := CheckCompiler(ctx, run); err == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	script := `set -eu
marker=/tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress
sudo -n touch "$marker"
trap 'sudo -n rm -f "$marker"' EXIT
label=''
attempt=1
while [ "$attempt" -le 6 ]; do
  label=$(/usr/sbin/softwareupdate --list 2>/dev/null | /usr/bin/sed -n 's/^\* Label: \(.*Command Line Tools.*\)$/\1/p' | /usr/bin/sort | /usr/bin/tail -1)
  [ -n "$label" ] && break
  /bin/sleep 15
  attempt=$((attempt + 1))
done
[ -n "$label" ]
sudo -n /usr/sbin/softwareupdate --install "$label"`
	if _, err := run(ctx, nil, "/bin/sh", "-c", script); err != nil {
		return fmt.Errorf("guest command line tools installation failed: %w", err)
	}
	return CheckCompiler(ctx, run)
}

// InstallXcode expands an archive already present on the command target and selects the installation.
func InstallXcode(ctx context.Context, run Command, archive string) error {
	if !filepath.IsAbs(archive) {
		return fmt.Errorf("macos: Xcode archive path must be absolute")
	}
	install := `set -eu
archive=$1
work=$(/usr/bin/mktemp -d /private/tmp/dockhand-xcode.XXXXXX)
trap '/bin/rm -rf "$work"' EXIT
/bin/mv "$archive" "$work/Xcode.xip"
cd "$work"
/usr/bin/xip --expand Xcode.xip
/bin/rm -f Xcode.xip
sudo -n /bin/rm -rf /Applications/Xcode.app
sudo -n /bin/mv Xcode.app /Applications/Xcode.app
sudo -n /usr/bin/xcode-select -s /Applications/Xcode.app/Contents/Developer
sudo -n /usr/bin/xcodebuild -license accept
sudo -n /usr/bin/xcodebuild -runFirstLaunch`
	if output, err := run(ctx, nil, "/bin/sh", "-c", install, "dockhand", archive); err != nil {
		return fmt.Errorf("macos: installing Xcode: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
