package provision

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

// xcodeBounds is the first Xcode version each release cannot run, by
// Darwin major — Apple raises the macOS floor partway through each
// Xcode line, so the bound is a minor, not a major. An absent entry
// means no known bound (the newest release runs the newest Xcode).
//
//	Monterey: Xcode 14.3 requires Ventura
//	Ventura:  Xcode 15.3 requires Sonoma
//	Sonoma:   Xcode 16.3 requires Sequoia
//	Sequoia:  Xcode 26.4 requires Tahoe 26.2
var xcodeBounds = map[int]string{
	21: "14.3",
	22: "15.3",
	23: "16.3",
	24: "26.4",
}

// PickXcode chooses the archive to install for a release: the newest
// .xip in dir whose version the release can run. dir may also name one
// .xip directly, which is then held to the same bound rather than
// trusted. Only release archives are considered — Apple's naming is
// Xcode_<version>.xip, and anything with more in the name (betas,
// release candidates) is skipped: a golden image is a verification
// environment, and verdicts from a beta toolchain answer a question
// nobody asked.
func PickXcode(dir string, r platform.Release) (path, version string, err error) {
	bound := xcodeBounds[r.Darwin]
	candidates := map[string]string{} // version -> path
	add := func(p string) {
		if v, ok := xipVersion(filepath.Base(p)); ok {
			candidates[v] = p
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", "", fmt.Errorf("%w: --xcode: %w", verify.ErrNoEnvironment, err)
	}
	if !info.IsDir() {
		add(dir)
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", "", err
		}
		for _, e := range entries {
			if !e.IsDir() {
				add(filepath.Join(dir, e.Name()))
			}
		}
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("%w: no Xcode_<version>.xip archives in %s", verify.ErrNoEnvironment, dir)
	}
	var best string
	for v := range candidates {
		if bound != "" && macports.VerCmp(v, bound) >= 0 {
			continue
		}
		if best == "" || macports.VerCmp(v, best) > 0 {
			best = v
		}
	}
	if best == "" {
		var all []string
		for v := range candidates {
			all = append(all, v)
		}
		return "", "", fmt.Errorf("%w: no archive %s can run: %s requires Xcode below %s, found %s",
			verify.ErrNoEnvironment, r.Name, r.Name, bound, strings.Join(all, ", "))
	}
	return candidates[best], best, nil
}

// xipVersion reads the version out of Apple's release archive naming,
// Xcode_<version>.xip. ok is false for anything else — including the
// beta and RC spellings, which carry more than a version.
func xipVersion(name string) (string, bool) {
	v, found := strings.CutPrefix(name, "Xcode_")
	if !found {
		return "", false
	}
	v, found = strings.CutSuffix(v, ".xip")
	if !found || v == "" {
		return "", false
	}
	for _, r := range v {
		if (r < '0' || r > '9') && r != '.' {
			return "", false
		}
	}
	return v, true
}

// installXcode pushes the archive into the guest over the provisioning
// SSH channel, expands it, and makes it the selected developer
// directory. The command line tools stay installed alongside — MacPorts
// wants both — but xcodebuild only works with a real Xcode selected,
// and use_xcode ports need xcodebuild.
//
// The expansion is the slow half: xip verifies Apple's signature over
// tens of gigabytes. Everything runs guest-side from /private/tmp, and
// the archive is deleted before the move so the peak disk need is one
// archive plus one Xcode.app.
func (t Tart) installXcode(ctx context.Context, vm, host, xip, version string, say func(string, ...any)) error {
	const guestXip = "/private/tmp/Xcode.xip"
	if fi, err := os.Stat(xip); err == nil {
		say("pushing Xcode %s into the guest (%.1f GB)", version, float64(fi.Size())/(1<<30))
	}
	if err := sshPush(ctx, host, xip, guestXip); err != nil {
		return fmt.Errorf("%w: pushing %s: %w", verify.ErrNoEnvironment, xip, err)
	}
	say("expanding and installing Xcode %s (xip verifies the whole archive; this is the slow part)", version)
	script := `set -e
avail=$(df -g /private/tmp | awk 'NR==2 {print $4}')
if [ "$avail" -lt 60 ]; then
  echo "only ${avail} GB free in the guest; Xcode needs about 60 to expand"
  exit 1
fi
cd /private/tmp
xip --expand ` + guestXip + `
rm -f ` + guestXip + `
sudo -n rm -rf /Applications/Xcode.app
sudo -n mv /private/tmp/Xcode.app /Applications/Xcode.app
sudo -n xcode-select -s /Applications/Xcode.app/Contents/Developer
sudo -n xcodebuild -license accept
sudo -n xcodebuild -runFirstLaunch`
	if out, err := tart.Exec(ctx, vm, "/bin/sh", "-c", script); err != nil {
		return fmt.Errorf("%w: installing Xcode %s: %s", verify.ErrNoEnvironment, version, strings.TrimSpace(out))
	}
	return t.assertXcode(ctx, vm)
}

// XcodeVersionOf reports the full Xcode a running image carries, ""
// when it has none — a fact worth a line wherever an image is already
// booted (recheck), because use_xcode ports are refused against a base
// without one.
func XcodeVersionOf(ctx context.Context, vm string) string {
	out, err := tart.Exec(ctx, vm, "/usr/bin/xcodebuild", "-version")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(out), "Xcode ") {
		return ""
	}
	first, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(strings.TrimPrefix(first, "Xcode"))
}

// assertXcode proves xcodebuild answers from a real Xcode — the same
// stance as every other provisioning step: the check, not the exit
// status, is what says the image has the capability.
func (t Tart) assertXcode(ctx context.Context, vm string) error {
	out, err := tart.Exec(ctx, vm, "/usr/bin/xcodebuild", "-version")
	if err != nil || !strings.Contains(out, "Xcode") {
		return fmt.Errorf("%w: xcodebuild does not answer from the installed Xcode: %s",
			verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	if out, err := tart.Exec(ctx, vm, "/usr/bin/clang", "--version"); err != nil ||
		!strings.Contains(out, "clang version") {
		return fmt.Errorf("%w: clang stopped answering after Xcode was selected: %s",
			verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	return nil
}
