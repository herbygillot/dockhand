package provision

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/build"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/session"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

// ImageVariant is the stock image dockhand builds from. Vanilla is the
// one without Homebrew, so there is nothing to remove and nothing to
// leave behind by accident. Its build template also installs the
// command line tools — usually; provisioning checks rather than trusts,
// because one published image was measured without them.
const ImageVariant = "vanilla"

// Tart provisions base images for the tart verifier.
type Tart struct {
	// CPUs and MemoryMB size each VM. Zero means the rule of thumb:
	// half the host's physical cores per VM — host-relative, because
	// everyone's machine is different and a constant tuned to one is
	// wrong on the rest — with memory following the cores at 2 GB
	// each, so the pair scales together.
	CPUs     int
	MemoryMB int
	// XcodeDir names a directory of Xcode .xip archives (or one .xip);
	// when set, the newest one the release can run is installed into
	// the image before the golden is taken. The archives are supplied
	// by the user because Apple's downloads sit behind an Apple ID —
	// the guest must never need credentials.
	XcodeDir string
	// MacPorts is the version installed into an image. Empty takes the
	// newest version dockhand has a shim for, which pins an environment
	// to something verified rather than to whatever is newest upstream
	// on the afternoon it was built.
	MacPorts string
	// Prefix is where MacPorts goes in the guest; the zero value is the
	// conventional one.
	Prefix prefix.Prefix
	// Tools resolves tart: the run's one finder, handed in by whoever
	// builds the provisioner, so provisioning drives the tart doctor
	// reported.
	Tools *tool.Finder
}

func (t Tart) prefixOf() prefix.Prefix {
	if t.Prefix == "" {
		return prefix.Prefix(macports.DefaultPrefix)
	}
	return t.Prefix
}

func (t Tart) macPortsVersion() (string, error) {
	if t.MacPorts != "" {
		return t.MacPorts, nil
	}
	v, err := session.NewestShim()
	if err != nil {
		return "", fmt.Errorf("%w: no MacPorts version given and no shim to infer one from: %w",
			verify.ErrNoEnvironment, err)
	}
	return v, nil
}

// imageRef is the stock image a release is built from.
func imageRef(r platform.Release) string {
	return fmt.Sprintf("ghcr.io/cirruslabs/macos-%s-%s:latest",
		strings.ToLower(r.CompactName()), ImageVariant)
}

// Provision builds the base image for a release and proves what it
// built. Progress goes to w because most of this is a download of tens
// of gigabytes, and a command silent that long is indistinguishable
// from a hung one.
func (t Tart) Provision(ctx context.Context, r platform.Release, w io.Writer) error {
	if r.IsZero() {
		return fmt.Errorf("%w: no release named", verify.ErrUnsupported)
	}
	version, err := t.macPortsVersion()
	if err != nil {
		return err
	}
	// Provisioning is a transaction against a TEMPORARY name: the
	// existing base survives untouched until its replacement has passed
	// every proof, so a capacity refusal, a network failure, or a
	// mid-provision crash costs nothing already working. The assessment
	// caught the old order deleting the base before admission had even
	// been asked.
	base := tart.BaseName(r)
	name := base + "-next"
	say := func(f string, a ...any) { fmt.Fprintf(w, f+"\n", a...) }

	say("pulling %s", imageRef(r))
	if out, err := tart.CLI(ctx, t.Tools, nil, "pull", imageRef(r)); err != nil {
		return fmt.Errorf("%w: pulling %s: %s", verify.ErrNoEnvironment, imageRef(r), strings.TrimSpace(out))
	}
	_, _ = tart.CLI(ctx, t.Tools, nil, "delete", name)
	if out, err := tart.CLI(ctx, t.Tools, nil, "clone", imageRef(r), name); err != nil {
		return fmt.Errorf("%w: cloning to %s: %s", verify.ErrNoEnvironment, name, strings.TrimSpace(out))
	}

	// Size before anything runs, so the golden inherits it too: a
	// restore must not quietly shrink a base back to image defaults.
	cpus, memMB := t.CPUs, t.MemoryMB
	if cpus == 0 || memMB == 0 {
		autoCPU, autoMem, physical := hostSizing()
		if cpus == 0 {
			cpus = autoCPU
		}
		if memMB == 0 {
			memMB = autoMem
		}
		say("sizing %s: %d cpus, %d MB (host has %d physical cores)", name, cpus, memMB, physical)
	} else {
		say("sizing %s: %d cpus, %d MB", name, cpus, memMB)
	}
	if cpus > 0 && memMB > 0 {
		if out, err := tart.CLI(ctx, t.Tools, nil, "set", name, "--cpu", strconv.Itoa(cpus), "--memory", strconv.Itoa(memMB)); err != nil {
			return fmt.Errorf("%w: sizing %s: %s", verify.ErrNoEnvironment, name, strings.TrimSpace(out))
		}
	}
	if t.XcodeDir != "" {
		// The vanilla images ship 50 GB disks with 13-24 GB free after
		// macOS and the toolchain — nowhere near Xcode's ~60 GB
		// expansion appetite, field-measured five for five. The disk
		// grows here (thin on the host: only written blocks cost) and
		// the APFS container inside grows after boot.
		say("growing the disk to %d GB (Xcode needs room to expand)", xcodeDiskGB)
		if out, err := tart.CLI(ctx, t.Tools, nil, "set", name, "--disk-size", strconv.Itoa(xcodeDiskGB)); err != nil {
			return fmt.Errorf("%w: growing %s's disk: %s", verify.ErrNoEnvironment, name, strings.TrimSpace(out))
		}
		// The guest recovery partition would sit between the container
		// and the new space; see gpt.go for why removing it pre-boot is
		// the only automatable path.
		img, err := diskImagePath(name)
		if err != nil {
			return err
		}
		if removed, err := removeRecoveryPartition(img); err != nil {
			return fmt.Errorf("%w: preparing %s's partition map: %w", verify.ErrNoEnvironment, name, err)
		} else if removed {
			say("removed the guest recovery partition (a VM never boots it; the space joins the container)")
		}
	}

	// Provisioning's boot spends a licence slot like any worker, so it
	// goes through the same admission: with builds in flight the answer
	// is a fast typed refusal, never a mid-provision hang.
	unlockAdmission, err := tart.Admit(ctx, t.Tools, tart.Provider{}.Capabilities().Concurrent)
	if err != nil {
		// Someone is standing here. Provisioning queues nothing and
		// records no run, so a refusal for want of a slot must not answer
		// as a deferral — `cycle` provisions nothing, and there is no run
		// for it to start. The provider counts slots and cannot know who
		// asked, so the caller that is waiting says so.
		var full *verify.CapacityError
		if errors.As(err, &full) {
			full.Synchronous = true
		}
		return err
	}
	runErr := make(chan error, 1)
	go func() {
		_, err := tart.CLI(context.WithoutCancel(ctx), t.Tools, nil, "run", "--no-graphics", name)
		runErr <- err
	}()
	defer func() { _, _ = tart.CLI(context.WithoutCancel(ctx), t.Tools, nil, "stop", name) }()
	if err := tart.WaitRunning(ctx, t.Tools, name, runErr); err != nil {
		unlockAdmission()
		return err
	}
	unlockAdmission()

	host, err := guestIP(ctx, t.Tools, name)
	if err != nil {
		return err
	}
	say("waiting for %s to accept a login", host)
	if err := waitSSH(ctx, host); err != nil {
		return fmt.Errorf("%w: %w", verify.ErrNoEnvironment, err)
	}

	// The one step that cannot use the agent, because it is what
	// installs the agent.
	say("installing the tart guest agent %s", AgentVersion)
	if out, err := sshRun(ctx, host, installAgentScript()); err != nil {
		return fmt.Errorf("%w: installing the guest agent: %w\n%s",
			verify.ErrNoEnvironment, err, strings.TrimSpace(out))
	}
	say("waiting for the agent to answer")
	if err := tart.WaitAgent(ctx, t.Tools, name); err != nil {
		return fmt.Errorf("%w: the agent was installed but does not answer: %w", verify.ErrNoEnvironment, err)
	}

	say("checking the image can compile")
	if err := t.ensureToolchain(ctx, name, say); err != nil {
		return err
	}
	xcodeNote := ""
	if t.XcodeDir != "" {
		xip, xv, err := PickXcode(t.XcodeDir, r)
		if err != nil {
			return err
		}
		if err := expandGuestDisk(ctx, t.Tools, name, say); err != nil {
			return err
		}
		if err := t.installXcode(ctx, name, host, xip, xv, say); err != nil {
			return err
		}
		xcodeNote = ", Xcode " + xv
	}
	say("installing MacPorts %s", version)
	if err := t.installMacPorts(ctx, name, r, version); err != nil {
		return err
	}
	say("verifying the image is what it claims")
	if err := t.assertPristine(ctx, name); err != nil {
		return err
	}
	// The golden is taken after the checks pass and before anything has
	// run the image, so it records a state that was verified rather than
	// merely reached. It is never started again.
	// Every proof has passed on the temporary image; only now does the
	// old pair give way. Golden first, then base, temp deleted last —
	// not atomic, but ordered so a crash at any point leaves either the
	// old pair intact or a proven image one free clone away.
	say("swapping the proven image in")
	golden := tart.GoldenName(r)
	if _, err := tart.CLI(context.WithoutCancel(ctx), t.Tools, nil, "stop", name); err != nil {
		slog.Debug("stopping before the swap", "vm", name, "err", err)
	}
	_, _ = tart.CLI(ctx, t.Tools, nil, "delete", golden)
	if out, err := tart.CLI(ctx, t.Tools, nil, "clone", name, golden); err != nil {
		return fmt.Errorf("%w: taking the golden copy %s: %s",
			verify.ErrNoEnvironment, golden, strings.TrimSpace(out))
	}
	_, _ = tart.CLI(ctx, t.Tools, nil, "delete", base)
	if out, err := tart.CLI(ctx, t.Tools, nil, "clone", name, base); err != nil {
		return fmt.Errorf("%w: installing the base %s: %s",
			verify.ErrNoEnvironment, base, strings.TrimSpace(out))
	}
	_, _ = tart.CLI(ctx, t.Tools, nil, "delete", name)

	say("provisioned %s — %s, MacPorts %s%s (golden: %s)", base, r, version, xcodeNote, golden)
	return nil
}

// guestIP waits for the guest to have an address, which is needed only
// for the SSH bootstrap.
func guestIP(ctx context.Context, tools *tool.Finder, vm string) (string, error) {
	out, err := tart.CLI(ctx, tools, nil, "ip", vm, "--wait", "300")
	if err != nil {
		return "", fmt.Errorf("%w: %s never got an address: %s",
			verify.ErrNoEnvironment, vm, strings.TrimSpace(out))
	}
	return strings.TrimSpace(out), nil
}

// installMacPorts fetches and installs the package for this release.
// The installer is per-release — its name carries both the product
// version and the marketing name — so the release decides which one.
func (t Tart) installMacPorts(ctx context.Context, vm string, r platform.Release, version string) error {
	pkg := "/tmp/" + build.InstallerName(version, r)
	script := fmt.Sprintf(`set -e
curl -fsSL -o %[1]s %[2]s
sudo -n installer -pkg %[1]s -target /
rm -f %[1]s
sudo -n %[3]s -v selfupdate`, pkg, build.InstallerURL(version, r), t.prefixOf().Port())
	if out, err := tart.Exec(ctx, t.Tools, vm, "/bin/sh", "-c", script); err != nil {
		return fmt.Errorf("%w: installing MacPorts: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	return nil
}

// assertPristine proves what the image claims rather than trusting that
// the steps meant to produce it worked. A half-provisioned image that
// went unnoticed would hand out verdicts from an environment nobody
// characterised, which is worse than no verdicts.
func (t Tart) assertPristine(ctx context.Context, vm string) error {
	paths := append(append([]string{}, build.ForeignPrefixes...), build.PathInjectors...)
	check := "for d in " + strings.Join(paths, " ") + `; do
  [ -e "$d" ] && echo "PRESENT: $d"
done
exit 0`
	out, err := tart.Exec(ctx, t.Tools, vm, "/bin/sh", "-c", check)
	if err != nil {
		return fmt.Errorf("%w: checking for foreign prefixes: %w", verify.ErrNoEnvironment, err)
	}
	if strings.Contains(out, "PRESENT") {
		return fmt.Errorf("%w: a foreign package manager is present:\n%s",
			verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	if out, err := tart.Exec(ctx, t.Tools, vm, t.prefixOf().Port(), "version"); err != nil ||
		!strings.Contains(out, "Version:") {
		return fmt.Errorf("%w: MacPorts does not answer: %s", verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	return t.assertToolchain(ctx, vm)
}

// ensureToolchain installs the command line tools when the image
// arrived without them, which one published image did.
//
// The tools are hidden from softwareupdate until a sentinel file says
// an on-demand install is under way — an Apple mechanism, and the same
// one the images' own build template uses.
//
// The label is matched loosely on purpose. The template greps for
// "Command Line Tools for Xcode-", with a hyphen straight after Xcode,
// and Tahoe offers "Command Line Tools for Xcode 26.6-26.6" with a
// space. That one character is why an image shipped with no compiler:
// the pattern matched nothing, xargs received nothing, and the install
// silently did nothing at all. Matching on "Command Line Tools" and
// taking the highest version survives both spellings and whatever comes
// next.
func (t Tart) ensureToolchain(ctx context.Context, vm string, say func(string, ...any)) error {
	if err := t.assertToolchain(ctx, vm); err == nil {
		return nil
	}
	say("no command line tools in the image; installing them (about a gigabyte)")
	// The label query retries, because a freshly booted guest's
	// softwareupdate can answer "no updates" for its first minute or so
	// while the catalog comes up — measured: the same query that failed
	// seconds after first boot succeeds on a guest that has been up a
	// while. An empty answer right after boot is "not yet", not "no".
	const install = `set -e
sudo -n touch /tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress
label=""
for attempt in 1 2 3 4 5 6; do
  label=$(softwareupdate --list 2>/dev/null | sed -n 's/^\* Label: \(.*Command Line Tools.*\)$/\1/p' | sort -V | tail -1)
  [ -n "$label" ] && break
  echo "softwareupdate offers nothing yet (attempt $attempt); waiting"
  sleep 15
done
if [ -z "$label" ]; then
  sudo -n rm -f /tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress
  echo "softwareupdate offers no command line tools"
  exit 1
fi
echo "installing: $label"
sudo -n softwareupdate --install "$label"
sudo -n rm -f /tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress`
	out, err := tart.Exec(ctx, t.Tools, vm, "/bin/sh", "-c", install)
	if err != nil {
		return fmt.Errorf("%w: installing the command line tools: %s",
			verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	// Prove it, rather than trusting softwareupdate's exit status: this
	// whole step exists because a silent no-op looked like success.
	if err := t.assertToolchain(ctx, vm); err != nil {
		return fmt.Errorf("%w (after installing them)", err)
	}
	return nil
}

// assertToolchain proves the image can compile, which nothing else here
// does. MacPorts installs from a package and answers `port version`
// without a compiler ever being present, so an image with no developer
// tools provisions cleanly, reports success, and then fails on the
// first port anybody verifies — opaquely, and once per port rather than
// once per image.
//
// Measured: today's macos-tahoe-vanilla ships with no
// /Library/Developer/CommandLineTools, no CLTools receipts, and a
// clang that only offers to install itself. The other four vanilla
// images carry the tools their own build template installs. So this is
// a property of a particular published image rather than of the
// variant, which is exactly the kind of thing an assertion is for and
// an assumption is not.
func (t Tart) assertToolchain(ctx context.Context, vm string) error {
	if out, err := tart.Exec(ctx, t.Tools, vm, "/usr/bin/xcode-select", "-p"); err != nil {
		return fmt.Errorf("%w: no developer tools in the image: %s\n"+
			"the published image may be missing the command line tools its template installs",
			verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	out, err := tart.Exec(ctx, t.Tools, vm, "/usr/bin/clang", "--version")
	if err != nil || !strings.Contains(out, "clang version") {
		return fmt.Errorf("%w: the image has no working compiler: %s",
			verify.ErrNoEnvironment, strings.TrimSpace(out))
	}
	return nil
}

// EnsureToolchainFor exposes the toolchain step for an image that
// already exists, so a base can be repaired without rebuilding it.
func (t Tart) EnsureToolchainFor(ctx context.Context, vm string, say func(string, ...any)) error {
	return t.ensureToolchain(ctx, vm, say)
}

// AssertPristineFor exposes the post-provisioning checks for an image
// that already exists, so a base can be re-checked without rebuilding it.
func (t Tart) AssertPristineFor(ctx context.Context, vm string) error {
	return t.assertPristine(ctx, vm)
}

// Provisioned lists the base images already built, which is what doctor
// reports as available platforms — the tool being installed says
// nothing about whether any environment exists.
func (t Tart) Provisioned(ctx context.Context) ([]platform.Release, error) {
	var found []platform.Release
	for _, r := range platform.Releases {
		ok, err := tart.HasVM(ctx, t.Tools, tart.BaseName(r))
		if err != nil {
			return nil, err
		}
		if ok {
			found = append(found, r)
		}
	}
	return found, nil
}

// Restore replaces a base with a fresh copy of its golden, which is the
// remedy when the pre-verification check finds a base has drifted.
//
// It is cheap enough to be the obvious response rather than a last
// resort: cloning under copy-on-write costs no meaningful time or disk,
// where re-provisioning costs a download.
func (t Tart) Restore(ctx context.Context, r platform.Release) error {
	golden, base := tart.GoldenName(r), tart.BaseName(r)
	out, err := tart.CLI(ctx, t.Tools, nil, "list", "--source", "local")
	if err != nil || !strings.Contains(out, golden) {
		return fmt.Errorf("%w: no golden copy %q to restore from; provision the release again",
			verify.ErrNoEnvironment, golden)
	}
	_, _ = tart.CLI(ctx, t.Tools, nil, "stop", base)
	_, _ = tart.CLI(ctx, t.Tools, nil, "delete", base)
	if out, err := tart.CLI(ctx, t.Tools, nil, "clone", golden, base); err != nil {
		return fmt.Errorf("%w: restoring %s from %s: %s",
			verify.ErrNoEnvironment, base, golden, strings.TrimSpace(out))
	}
	return nil
}
