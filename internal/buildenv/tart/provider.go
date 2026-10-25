// Package tart is the Tart provider (Design v3 §7): it builds a check's
// targets in a fresh clone of one of dockhand's prepared images, one guest
// for each release and attempt, in MacPorts CI's order (decisions 11 and
// 22). The revision's tree and a port index for the release are staged
// into the guest, a program there builds the targets and writes each
// result as it finishes, and the provider records each as it appears, so a
// guest lost midway keeps what it finished (decision 44). The clone is
// deleted after, and an attempt removes what an earlier one of its run
// left behind.
package tart

import (
	"archive/tar"
	"cmp"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	guestprogram "github.com/herbygillot/dockhand/internal/buildenv/guest"
	"github.com/herbygillot/dockhand/internal/buildenv/staging"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/binaryarchive"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/refusal"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/guestssh"
)

// VerifierProtocol identifies how the guest program builds: part of an
// environment's origin, so raising it ends reuse of evidence the program
// recorded before (decision 28). A test pins guest.tcl, and fails until a
// change to it raises this or, for a change of wording only, re-pins.
const VerifierProtocol = 3

// guestRoot is where the guest program and the staged tree live in a clone.
const guestRoot = "/var/tmp/dockhand-check"

// guestLabel is the launchd job the guest program runs as.
const guestLabel = "org.dockhand.check"

// Provider builds on dockhand's Tart images.
type Provider struct {
	// Tart is dockhand's Tart installation and home, Personal the
	// person's own Tart home, which dockhand only reads, and Keys the
	// folder of the SSH keys guests are reached with, as compose places
	// them.
	Tart     tartvm.Client
	Personal string
	Keys     string
	Repo     *git.Repository
	// Index stages a tree's port index for a platform: the engine's.
	Index func() (portindex.Source, error)
	// Prefix is where the images' MacPorts is, /opt/local when empty.
	Prefix string
	// TestTimeout bounds a target's tests, 30 minutes when zero.
	TestTimeout time.Duration
	// BuildTimeout bounds a target's build, its dependencies' installs,
	// fetch, checksum, and install, 6 hours when zero (D16).
	BuildTimeout time.Duration
	// Poll is how often a guest's results are read, 10 seconds when zero.
	Poll time.Duration

	workspaces workspace.Registry
	// machine replaces the Mac's VMs in tests, and stager the staging.
	machine machine
	stager  func(ctx context.Context, job buildenv.Job, input guestprogram.Input, archive string) error
	// archiveKeys sign the archives guests install; dockhand's own when
	// nil.
	archiveKeys func() (binaryarchive.Keys, error)
	// assembling guards making the Mac's machine on first use (vms), which
	// two environments building together may ask for at once.
	assembling sync.Mutex
	// host is this Mac's Darwin release; the kernel's when zero.
	host int
	// starting lets one clone start at a time (start).
	starting sync.Mutex
	// xcodes stands in for xcodes in tests.
	xcodes string
	// hostMacPorts is this Mac's MacPorts version, read once; macPorts
	// stands in for it in tests.
	hostMacPorts sync.Once
	macPorts     string
}

// follower follows the guest program in a clone: its results in
// guestRoot, its end launchd's job's, its tools compared with the facts
// table's, and its dependencies' archives kept for later guests.
func (p *Provider) follower() *guestprogram.Follower {
	return &guestprogram.Follower{Prefix: p.prefix(), Root: guestRoot, Poll: p.poll(), Stopped: stopped, Drift: true, KeepDependencies: true}
}

func (p *Provider) Name() string { return buildenv.Tart }

func (p *Provider) vms() (machine, error) {
	p.assembling.Lock()
	defer p.assembling.Unlock()
	if p.machine != nil {
		return p.machine, nil
	}
	client, err := p.Tart.Resolve()
	if err != nil {
		return nil, err
	}
	keys, err := p.sshKeys()
	if err != nil {
		return nil, err
	}
	p.machine = newNative(client, keys, p.Personal)
	return p.machine, nil
}

// The Tart provider builds on the releases a person names, says what each
// image is made from, says how to give a release Xcode, and removes the
// clones a stopped check left.
var (
	_ buildenv.IdentityProvider = (*Provider)(nil)
	_ buildenv.CacheProvider    = (*Provider)(nil)
	_ buildenv.ParallelProvider = (*Provider)(nil)
	_ buildenv.ReleaseProvider  = (*Provider)(nil)
	_ buildenv.Remedier         = (*Provider)(nil)
	_ buildenv.LeftoverProvider = (*Provider)(nil)
)

// Environments are the releases --on tart:<releases> selects, each with its
// image (decision 6): the Mac's own release with none (decision 4), every
// release with an image for "all", or the ones named, by name or product
// version. A release without its base image is refused with the command
// that makes one. Xcode is an add-on: a release with its Xcode image builds
// there, with Xcode, and one without it with the Command Line Tools alone.
func (p *Provider) Environments(ctx context.Context, releases string) ([]model.Environment, error) {
	m, err := p.vms()
	if err != nil {
		return nil, err
	}
	// A Tart older than setup takes is refused before any VM starts, as
	// setup refuses it: check-100 started one on 2.38.0 and passed, where
	// its listing can block while an ASIF VM runs (the rc9 full stage's
	// F8, openai/tart#1344).
	version, err := m.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading Tart's version: %w", err)
	}
	if err := tartvm.TooOld(version); err != nil {
		return nil, err
	}
	images, err := m.Images(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing dockhand's Tart images: %w", err)
	}
	var chosen []macos.Release
	switch releases {
	case "":
		darwin, err := p.hostRelease()
		if err != nil {
			return nil, err
		}
		release, err := macos.ReleaseForDarwin(darwin)
		if err != nil {
			return nil, fmt.Errorf("this Mac's macOS: %w", err)
		}
		chosen = []macos.Release{release}
	case "all":
		for _, release := range macos.Known() {
			if slices.Contains(images, baseImage(release)) {
				chosen = append(chosen, release)
			}
		}
		if len(chosen) == 0 {
			return nil, refusal.New(refusal.Environment, "--on tart:all: no Tart base images are available; setup makes one for this Mac's macOS").
				Then(refusal.Next{Verb: refusal.VerbSetup, Args: []string{"tart"}})
		}
	default:
		for _, name := range strings.Split(releases, ",") {
			release, err := macos.ParseRelease(name)
			if err != nil {
				return nil, fmt.Errorf("--on tart:%s: %w", releases, err)
			}
			chosen = append(chosen, release)
		}
	}
	var environments []model.Environment
	for _, release := range chosen {
		if !slices.Contains(images, baseImage(release)) {
			// Where its golden copy is kept, setup restores it in about a
			// minute, which "makes" read as an hour's work. check doesn't
			// restore it itself: that writes tens of gigabytes unasked (the
			// person's decision on D-R3, 2026-10-16).
			if golden := tartvm.GoldenName(baseImage(release)); slices.Contains(images, golden) {
				return nil, refusal.New(refusal.Environment, "no Tart image for macOS %s (%s); setup restores %s from %s in about a minute", release.Product, release.Name, baseImage(release), golden).
					Then(refusal.Next{Verb: refusal.VerbSetup, Args: []string{"tart", release.Slug}})
			}
			return nil, refusal.New(refusal.Environment, "no Tart image for macOS %s (%s); setup creates %s", release.Product, release.Name, baseImage(release)).
				Then(refusal.Next{Verb: refusal.VerbSetup, Args: []string{"tart", release.Slug}})
		}
		environment := model.Environment{Provider: buildenv.Tart, Platform: model.Platform{OS: "darwin", Version: strconv.Itoa(release.Darwin), Architecture: "arm64"},
			DeveloperTools: model.DeveloperToolsCommandLine}
		if slices.Contains(images, xcodeImage(release)) {
			environment.DeveloperTools = model.DeveloperToolsXcode
		}
		if !slices.Contains(environments, environment) {
			environments = append(environments, environment)
		}
	}
	return environments, nil
}

// hostRelease is this Mac's Darwin major version.
func (p *Provider) hostRelease() (int, error) {
	if p.host != 0 {
		return p.host, nil
	}
	return hostDarwin()
}

// baseImage is the image setup makes for a release, with the Command Line
// Tools alone.
func baseImage(release macos.Release) string {
	return tartvm.Prepared{Release: release, Profile: macos.ProfileTools}.Name()
}

// image is the image an environment builds in (decision 23, amended): the
// release's Xcode image when the environment has Xcode, and its base image
// otherwise.
func image(release macos.Release, environment model.Environment) string {
	if environment.DeveloperTools == model.DeveloperToolsXcode {
		return xcodeImage(release)
	}
	return baseImage(release)
}

// MacPortsBehind says where an environment's image has an older MacPorts
// than this Mac's, which reads the ports it builds
// (buildenv.MacPortsProvider): the image's version and this Mac's, or
// empty where it isn't behind, or either can't be told. An image keeps the
// MacPorts it was made with (the person's "yes", 2026-10-16: #35553 was
// checked on a Tahoe image's 2.12.6 while this Mac had 2.12.7, unsaid).
func (p *Provider) MacPortsBehind(ctx context.Context, environment model.Environment) (imageVersion, hostVersion string) {
	release, err := tartvm.ReleaseForPlatform(environment.Platform)
	if err != nil {
		return "", ""
	}
	runtime, err := p.Tart.Resolve()
	if err != nil {
		return "", ""
	}
	manifest, found, err := tartvm.ReadImageRecord(runtime.Home, image(release, environment))
	if err != nil || !found {
		return "", ""
	}
	return p.behind(ctx, manifest.MacPortsVersion)
}

// behind is an image's MacPorts version and this Mac's, where the image's
// is older.
func (p *Provider) behind(ctx context.Context, version string) (imageVersion, hostVersion string) {
	p.hostMacPorts.Do(func() {
		if p.macPorts == "" {
			p.macPorts = HostMacPorts(ctx, p.tclsh(), p.Tart.Env)
		}
	})
	if version == "" || p.macPorts == "" || macports.BaseAtLeast(version, p.macPorts) {
		return "", ""
	}
	return version, p.macPorts
}

// Identity is an environment's identity by origin
// (buildenv.IdentityProvider): its image's origin, as setup recorded it,
// and the guest program's protocol. Empty for an image setup recorded no
// origin of.
func (p *Provider) Identity(_ context.Context, environment model.Environment) (string, error) {
	release, err := tartvm.ReleaseForPlatform(environment.Platform)
	if err != nil {
		return "", err
	}
	runtime, err := p.Tart.Resolve()
	if err != nil {
		return "", err
	}
	manifest, found, err := tartvm.ReadImageRecord(runtime.Home, image(release, environment))
	if err != nil || !found || manifest.Origin() == "" {
		return "", err
	}
	return fmt.Sprintf("%s; verifier %d", manifest.Origin(), VerifierProtocol), nil
}

// verifierChanges are what each guest protocol changed in how it builds,
// for a person told why an earlier result doesn't stand.
var verifierChanges = map[int]string{
	2: "has begun to build each target from its source, never from a published archive, and from clean work",
	3: "has begun to build each target with its own dependencies alone active, deactivating what a dependency built from source needed to build",
}

// IdentityChange says what changed between two of an environment's
// identities where only its guest protocol did: the image is the one it
// was, and dockhand builds in it otherwise now (buildenv.IdentityExplainer).
// A changed image is left for the engine to say.
func (p *Provider) IdentityChange(_ model.Environment, recorded, now string) string {
	recordedImage, recordedProtocol, ok := strings.Cut(recorded, "; verifier ")
	nowImage, nowProtocol, nowOK := strings.Cut(now, "; verifier ")
	if !ok || !nowOK || recordedImage != nowImage || recordedProtocol == nowProtocol {
		return ""
	}
	protocol, err := strconv.Atoi(nowProtocol)
	if err != nil {
		return ""
	}
	if change, known := verifierChanges[protocol]; known {
		return "dockhand " + change
	}
	return "dockhand has begun to build otherwise"
}

// Storage is dockhand's Tart home, where Tart keeps the vanilla images
// setup pulled (buildenv.CacheProvider).
func (p *Provider) Storage() (string, error) {
	runtime, err := p.Tart.Resolve()
	return runtime.Home, err
}

// PruneCache deletes the vanilla images Tart pulled for dockhand that have
// gone unused for longer than unused, one by one, never with tart prune
// (decision 36). Only digests are judged: Tart marks the digest it opens,
// not the tag that named it, so a tag's own time says nothing of its use,
// and deleting a digest takes its tags with it. One Tart gives no time
// for, or that is running, is kept.
func (p *Provider) PruneCache(ctx context.Context, unused time.Duration) ([]string, error) {
	m, err := p.vms()
	if err != nil {
		return nil, err
	}
	cached, err := m.Cached(ctx)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-unused)
	var removed []string
	for _, image := range cached {
		if !strings.Contains(image.Name, "@sha256:") || image.Running || image.Accessed.IsZero() || image.Accessed.After(cutoff) {
			continue
		}
		if err := m.DeleteCached(ctx, image.Name); err != nil {
			return removed, fmt.Errorf("deleting %s from Tart's cache: %w", image.Name, err)
		}
		removed = append(removed, image.Name)
	}
	return removed, nil
}

// Remedy is the command that gives a release what an unmet target needs:
// its Xcode image.
func (p *Provider) Remedy(unmet model.Unmet) string {
	release, err := tartvm.ReleaseForPlatform(unmet.Environment.Platform)
	switch {
	case err != nil:
		return ""
	case unmet.Needs == model.RequiresXcode:
		return fmt.Sprintf("dockhand setup tart %s --xcode <Xcode .xip, or a folder of them> makes macOS %s's Xcode image", release.Slug, release.Product)
	case unmet.Needs.XcodeVersion() != "":
		version := unmet.Needs.XcodeVersion()
		return fmt.Sprintf("%s = %q under [providers.tart.xcode], then dockhand setup tart %s --xcode <Xcode %s .xip>, gives macOS %s's Xcode image Xcode %s", release.Slug, version, release.Slug, version, release.Product, version)
	}
	return ""
}

// Leftovers are the check clones in dockhand's Tart home. An attempt
// deletes its clone when it ends, and a run's next attempt deletes an
// earlier one's, so a clone found here is in use or was left by a process
// that died; the engine tells which by the clone's check. The images checks
// clone from, and anything else in the home, are never listed.
func (p *Provider) Leftovers(ctx context.Context) ([]buildenv.Leftover, error) {
	m, err := p.vms()
	if err != nil {
		return nil, err
	}
	images, err := m.Images(ctx)
	if err != nil {
		return nil, err
	}
	home, _ := p.home()
	var clones []buildenv.Leftover
	for _, name := range images {
		if strings.HasPrefix(name, clonePrefixAll) {
			leftover := buildenv.Leftover{Ref: name, What: "Tart clone " + name}
			if at, kept := tartvm.KeptAt(home, name); kept && home != "" {
				leftover.KeptAt, leftover.What = at, "kept Tart clone "+name
			}
			clones = append(clones, leftover)
		}
	}
	return clones, nil
}

// home is dockhand's Tart home, resolved.
func (p *Provider) home() (string, error) {
	client, err := p.Tart.Resolve()
	return client.Home, err
}

// restage replaces the guest's staged port directory with the branch's,
// directory, as it is now.
func (p *Provider) restage(ctx context.Context, g guest, directory, staged string) error {
	archive, err := os.CreateTemp("", "dockhand-port-*.tar")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	err = tarDirectory(archive, directory)
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("packing %s: %w", directory, err)
	}
	upload := guestRoot + "/shell-port.tar"
	if err := g.Upload(ctx, archive.Name(), upload, true); err != nil {
		return err
	}
	if output, err := g.Command(ctx, nil, "/usr/bin/sudo", "/bin/sh", "-c", `rm -rf "$1" && mkdir -p "$1" && tar -xf "$2" -C "$1" && rm -f "$2"`, "dockhand", staged, upload); err != nil {
		return fmt.Errorf("putting the port in place: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// tarDirectory writes directory's regular files and folders to out as a
// tar archive, named from it.
func tarDirectory(out io.Writer, directory string) error {
	writer := tar.NewWriter(out)
	err := filepath.WalkDir(directory, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || file == directory {
			return err
		}
		info, err := entry.Info()
		if err != nil || !(info.Mode().IsRegular() || info.IsDir()) {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, file)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if err := writer.WriteHeader(header); err != nil || info.IsDir() {
			return err
		}
		data, err := os.Open(file)
		if err != nil {
			return err
		}
		defer data.Close()
		_, err = io.Copy(writer, data)
		return err
	})
	if err != nil {
		return err
	}
	return writer.Close()
}

// keepClone marks a check's clone as kept, which is then left for dockhand
// shell, and for clean or the branch's next check to remove.
func (p *Provider) keepClone(vm, image string) error {
	home, err := p.home()
	if err != nil {
		return err
	}
	return tartvm.KeepClone(home, vm, image)
}

// Shell starts a clone a check kept (keepClone), runs job's command in the
// port's work directory, else its staged directory, streaming what it
// says, and stops the clone again, returning the command's exit status.
// Any clone but a kept one is refused.
func (p *Provider) Shell(ctx context.Context, job buildenv.ShellJob) (int, error) {
	home, err := p.home()
	if err != nil {
		return -1, err
	}
	image, kept := tartvm.KeptFrom(home, job.Ref)
	if !kept || !strings.HasPrefix(job.Ref, clonePrefixAll) {
		return -1, fmt.Errorf("%s isn't a clone a check kept; check --keep keeps one", job.Ref)
	}
	m, err := p.vms()
	if err != nil {
		return -1, err
	}
	started, err := m.Start(job.Ref)
	if err != nil {
		return -1, fmt.Errorf("starting %s: %w", job.Ref, err)
	}
	cleanup := context.WithoutCancel(ctx)
	defer func() { _ = started.Stop(cleanup, time.Minute) }()
	g, err := m.Reach(ctx, job.Ref, image)
	if err != nil {
		return -1, fmt.Errorf("reaching %s: %w", job.Ref, err)
	}
	defer g.Close(cleanup)
	if err := guestssh.AwaitSSH(ctx, g, started, 4*time.Minute, 3*time.Second); err != nil {
		return -1, fmt.Errorf("reaching %s: %w", job.Ref, err)
	}
	staged := path.Join(guestRoot, "ports", path.Dir(job.Portfile))
	command := job.Command
	if job.Test != "" {
		if err := p.restage(ctx, g, job.Test, staged); err != nil {
			return -1, err
		}
		command = []string{"/usr/bin/sudo", p.prefix() + "/bin/port", "-o", "-N", "-D", staged, "test"}
		command = append(command, strings.Fields(job.Variants)...)
	}
	if len(command) == 0 {
		command = []string{"/bin/zsh", "-l"}
	}
	// The work directory is the port's as MacPorts places it, read in the
	// guest; the staged directory where it's gone.
	script := `work=$(` + p.prefix() + `/bin/port -q -D "$1" work 2>/dev/null); cd "${work:-$1}" 2>/dev/null || cd "$1"; shift; exec "$@"`
	return g.Attach(ctx, job.Input, job.Output, job.Terminal, "", append([]string{"/bin/sh", "-c", script, "dockhand", staged}, command...)...)
}

// RemoveLeftover stops and deletes one check clone. Any other name is
// refused, so no image a check clones from is ever touched.
func (p *Provider) RemoveLeftover(ctx context.Context, ref string) error {
	if !strings.HasPrefix(ref, clonePrefixAll) {
		return fmt.Errorf("%s is not a temporary build VM; only temporary build VMs can be removed", ref)
	}
	m, err := p.vms()
	if err != nil {
		return err
	}
	if err := m.Stop(ctx, ref); err != nil {
		return err
	}
	if err := m.Delete(ctx, ref); err != nil {
		return err
	}
	if home, err := p.home(); err == nil {
		return tartvm.ForgetClone(home, ref)
	}
	return nil
}

// Execute builds the job's targets in a fresh clone of the release's image.
// Trouble with the VM or the guest is an infrastructure error, which the
// runner tries again; a target that fails to build is a result.
func (p *Provider) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) (err error) {
	m, err := p.vms()
	if err != nil {
		return buildenv.Infrastructure("%w", err)
	}
	release, err := tartvm.ReleaseForPlatform(job.Environment.Platform)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(job.Directory, 0o755); err != nil {
		return err
	}
	image := image(release, job.Environment)
	prefix := clonePrefix(string(job.Run.ID), release.Slug)
	vm := vmName(prefix, job.Execution.Attempt)
	cleanup := context.WithoutCancel(ctx)
	p.sweep(cleanup, m, prefix, job.Execution.Attempt)

	input := guestprogram.Input{Protocol: guestprogram.Protocol, Run: job.Run.Name(), Attempt: job.Execution.Attempt, Prefix: p.prefix(),
		Platform: job.Environment.Platform, Tests: string(job.Plan.Tests), TestTimeout: int(p.testTimeout() / time.Second),
		BuildTimeout: int(p.buildTimeout() / time.Second), LintTimeout: int(guestprogram.LintTimeout / time.Second)}
	if input.Tests == "" {
		input.Tests = string(model.TestsDeclared)
	}
	input.Targets = guestprogram.Targets(job, build)
	// With nothing to build, no VM starts; the runner records what's
	// blocked.
	if !slices.ContainsFunc(input.Targets, func(t guestprogram.Target) bool { return !t.Blocked }) {
		return nil
	}
	for _, archive := range job.Installs {
		if !binaryarchive.Installable(archive.Port, archive.Name) {
			return fmt.Errorf("an archive of %q named %q can't be installed", archive.Port, archive.Name)
		}
		input.Archives = append(input.Archives, guestprogram.Archive{Port: archive.Port, Name: archive.Name})
	}
	if len(input.Archives) > 0 {
		input.ArchiveSite, input.ArchiveKeys = archiveSite, []string{path.Join(archiveSite, binaryarchive.RSAPublicKey), path.Join(archiveSite, binaryarchive.SignifyPublicKey)}
	}

	build.Progress("staging the revision for " + release.Name)
	archive := filepath.Join(job.Directory, "input.tar")
	defer os.Remove(archive)
	stage := p.stage
	if p.stager != nil {
		stage = p.stager
	}
	if err := stage(ctx, job, input, archive); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("staging the revision: %w", err)
	}

	cloned, started, err := p.start(ctx, m, build, image, vm)
	if cloned {
		defer func() {
			// A check asked to keep its clone keeps it, stopped, where the
			// guest finished, its targets passed or failed: never after
			// trouble, where the guest didn't (model.Plan.Keep).
			if job.Plan.Keep && err == nil {
				keepErr := p.keepClone(vm, image)
				if keepErr == nil {
					return
				}
				build.Progress("keeping " + vm + ": " + keepErr.Error())
			}
			if deleteErr := m.Delete(cleanup, vm); deleteErr != nil && err == nil {
				build.Progress("deleting " + vm + ": " + deleteErr.Error())
			}
		}()
	}
	if err != nil {
		return err
	}
	defer func() { _ = started.Stop(cleanup, time.Minute) }()
	// A VM whose tart run ended takes no address: waiting out tart ip's
	// five minutes for one only delayed saying so (the rc5 full stage).
	reaching, stopReaching := context.WithCancel(ctx)
	go func() {
		select {
		case <-started.Done():
			stopReaching()
		case <-reaching.Done():
		}
	}()
	g, err := m.Reach(reaching, vm, image)
	stopReaching()
	if err != nil {
		select {
		case <-started.Done():
			err = fmt.Errorf("tart run exited before %s received an IP address: %w", vm, cmp.Or(started.Err(), err))
		default:
		}
		return p.trouble(ctx, "reaching "+vm, err)
	}
	defer g.Close(cleanup)
	if err := guestssh.AwaitSSH(ctx, g, started, 4*time.Minute, 3*time.Second); err != nil {
		return p.trouble(ctx, "reaching "+vm, err)
	}
	if err := p.install(ctx, g, job, build); err != nil {
		return p.trouble(ctx, "giving "+vm+" the archives it installs", err)
	}
	// With the Tart home that reaches it, which a person's tart list
	// doesn't see (the bazel session's notes, 2026-10-24).
	building := "building in " + vm
	if home, err := p.home(); err == nil && home != "" {
		building += fmt.Sprintf(" (TART_HOME=%s tart exec %s …)", home, vm)
	}
	build.Progress(building)
	if err := launch(ctx, g, archive); err != nil {
		return p.trouble(ctx, "starting the build in "+vm, err)
	}
	return p.follower().Follow(ctx, g, started, job, build, release)
}

func (p *Provider) prefix() string {
	if p.Prefix != "" {
		return p.Prefix
	}
	return macports.DefaultPrefix
}

func (p *Provider) testTimeout() time.Duration {
	if p.TestTimeout > 0 {
		return p.TestTimeout
	}
	return buildenv.DefaultTestTimeout
}

func (p *Provider) buildTimeout() time.Duration {
	if p.BuildTimeout > 0 {
		return p.BuildTimeout
	}
	return buildenv.DefaultBuildTimeout
}

func (p *Provider) poll() time.Duration {
	if p.Poll > 0 {
		return p.Poll
	}
	return 10 * time.Second
}

// trouble is an infrastructure error, unless the run was stopped.
func (p *Provider) trouble(ctx context.Context, doing string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, guestssh.ErrRefused) {
		// A guest that refuses dockhand refuses it again: the image's keys
		// or password are another's, which another attempt won't change.
		return buildenv.Infrastructure("%w: %s: %w", buildenv.ErrNeedsAttention, doing, err)
	}
	return buildenv.Infrastructure("%s: %w", doing, err)
}

// sweep stops and deletes the clones earlier attempts of this run and
// release left when their process died. Their attempts are over: the
// runner starts a new one only after settling the last.
func (p *Provider) sweep(ctx context.Context, m machine, prefix string, attempt int) {
	images, err := m.Images(ctx)
	if err != nil {
		return
	}
	for _, name := range images {
		earlier, ok := strings.CutPrefix(name, prefix+"-")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(earlier); err != nil || n >= attempt {
			continue
		}
		_ = m.Stop(ctx, name)
		_ = m.Delete(ctx, name)
	}
}

// start takes a slot on the Mac, then clones the image and starts the
// clone, one start at a time in this process, and until the clone is
// listed as running: releases checked together would otherwise each see a
// slot free before either's VM runs. It says whether it cloned, for the
// caller to delete what it made.
func (p *Provider) start(ctx context.Context, m machine, build buildenv.Build, image, vm string) (cloned bool, started run, err error) {
	p.starting.Lock()
	defer p.starting.Unlock()
	before, err := p.slot(ctx, m, build)
	if err != nil {
		return false, nil, err
	}
	build.Progress("starting " + vm + " from " + image)
	// The clone is named on the execution before it exists, so one this
	// process leaves when it dies is always a known check's (Leftovers).
	if err := build.Refer(vm); err != nil {
		return false, nil, err
	}
	if err := m.Clone(ctx, image, vm); err != nil {
		return false, nil, buildenv.Infrastructure("cloning %s: %w", image, err)
	}
	if started, err = m.Start(vm); err != nil {
		return true, nil, buildenv.Infrastructure("starting %s: %w", vm, err)
	}
	// The next start counts this VM once Tart lists it, which takes a
	// moment; it waits that long at most.
	for range 30 {
		if running, err := m.Running(ctx); err != nil || running > before {
			break
		}
		select {
		case <-ctx.Done():
			return true, started, nil
		case <-time.After(min(p.poll(), time.Second)):
		}
	}
	return true, started, nil
}

// Parallel is how many releases one check builds at once: the Mac runs two
// VMs at most (buildenv.ParallelProvider). One that finds the other slot
// taken, by the person's own VM or another check's, waits for it.
func (p *Provider) Parallel() int { return 2 }

// slot waits until the Mac has room for another VM: it runs two at most,
// whoever started them. It says how many were running then.
func (p *Provider) slot(ctx context.Context, m machine, build buildenv.Build) (int, error) {
	told := false
	for {
		running, err := m.Running(ctx)
		if err != nil {
			return 0, p.trouble(ctx, "counting the Mac's running VMs", err)
		}
		if running < 2 {
			return running, nil
		}
		if !told {
			build.Waiting(fmt.Sprintf("waiting for the Mac's VMs: %d are running, and macOS runs two at most%s", running, whoseWords(ctx, m, build)))
			told = true
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(3 * p.poll()):
		}
	}
}

// whoseWords say whose the running VMs are, the checks dockhand's run and
// how many are the person's own: "check-616's tart-abc, and 1 of yours".
// Nothing said whose they were, so a session's environments ran in turn
// behind other sessions' checks with no word why (the exiv2 and bazel
// sessions' notes, 2026-10-24). Empty where it can't be read.
func whoseWords(ctx context.Context, m machine, build buildenv.Build) string {
	ours, theirs, err := m.Whose(ctx)
	if err != nil || len(ours)+theirs == 0 {
		return ""
	}
	var words []string
	for _, vm := range ours {
		if check := build.Holding(vm); check != "" {
			words = append(words, check+"'s "+vm)
		} else {
			words = append(words, "dockhand's "+vm)
		}
	}
	if theirs > 0 {
		words = append(words, fmt.Sprintf("%d of yours", theirs))
	}
	return ": " + prose.And(words)
}

// stage packs the revision's tree, its port index for the release, and
// the guest program and its input into one archive.
func (p *Provider) stage(ctx context.Context, job buildenv.Job, input guestprogram.Input, archive string) error {
	if len(job.Targets) == 0 {
		return errors.New("no targets")
	}
	if p.Index == nil {
		return errors.New("no port index source")
	}
	index, err := p.Index()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return err
	}
	var rest []model.Target
	for _, target := range job.Targets[1:] {
		rest = append(rest, target.Target)
	}
	payload := map[string][]byte{"guest.tcl": guestprogram.Program, "input.json": data, "guest.plist": guestPlist(input.Prefix)}
	return staging.Archive(ctx, p.Repo, &p.workspaces, staging.Request{Source: job.Revision.Source, Target: job.Targets[0].Target, AdditionalTargets: rest,
		Platform: job.Environment.Platform, Index: index}, archive, payload, nil)
}

func guestPlist(prefix string) []byte {
	return macos.LaunchdPlist(guestLabel, []string{prefix + "/bin/port-tclsh", guestRoot + "/guest.tcl"}, guestRoot+"/runner.log",
		map[string]string{"PATH": prefix + "/bin:" + prefix + "/sbin:/usr/bin:/bin:/usr/sbin:/sbin"})
}

// archiveSite is where a guest's kept archives are: an archive site of
// MacPorts' kind, each archive in its port's directory, which the guest
// program has MacPorts try first.
const archiveSite = "/var/tmp/dockhand-archives"

// install gives the guest the kept archives it installs targets from
// (decision 28): each checked against its digest again, signed with
// dockhand's archive keys both ways MacPorts verifies an archive site's,
// and copied in beside its signatures, with the keys' public halves.
// MacPorts reads them as its unprivileged user, so they are made readable
// to all; the guest is a clone that goes with its check.
func (p *Provider) install(ctx context.Context, g guest, job buildenv.Job, build buildenv.Build) error {
	if len(job.Installs) == 0 {
		return nil
	}
	keys, err := p.signingKeys()
	if err != nil {
		return err
	}
	directories := []string{"sudo", "-n", "/bin/mkdir", "-p"}
	for _, archive := range job.Installs {
		directories = append(directories, path.Join(archiveSite, archive.Port))
	}
	if _, err := g.Command(ctx, nil, directories...); err != nil {
		return err
	}
	for _, archive := range job.Installs {
		build.Progress(givingWords(archive))
		// Signed once, beside the kept archive, which cleanup removes them
		// with; its digest is checked each time (D18).
		entry, err := binaryarchive.Sign(ctx, keys, binaryarchive.Archive{Port: archive.Port, Name: archive.Name, Digest: archive.Digest, Path: archive.Path})
		if err != nil {
			return fmt.Errorf("preparing the saved archive for %s: %w", archive.Target, err)
		}
		for _, name := range slices.Sorted(maps.Keys(entry.Files)) {
			if err := g.Upload(ctx, entry.Files[name], binaryarchive.EntryPath(archiveSite, entry.Port, name), true); err != nil {
				return err
			}
		}
	}
	for name, public := range keys.PublicKeys() {
		local := filepath.Join(job.Directory, name)
		err := os.WriteFile(local, public, 0o600)
		if err == nil {
			err = g.Upload(ctx, local, path.Join(archiveSite, name), true)
		}
		os.Remove(local)
		if err != nil {
			return err
		}
	}
	_, err = g.Command(ctx, nil, "sudo", "-n", "/bin/chmod", "-R", "a+rX", archiveSite)
	return err
}

// givingWords say an archive given to the guest: a target's is its
// build's; a dependency's, offered for MacPorts to take if it's the one
// it wants, is an earlier guest's install of it (batch 90), and has no
// target to name, which left "giving the guest , from …" (the M1's run at
// 11491d4e).
func givingWords(archive buildenv.Archive) string {
	if archive.Target == "" {
		return fmt.Sprintf("giving the guest %s, from the archive an earlier guest installed it from", archive.Name)
	}
	return fmt.Sprintf("giving the guest %s, from the archive kept of its build", archive.Target)
}

// signingKeys are the keys archives given to guests are signed with, kept
// beside dockhand's SSH keys, where they have always been.
func (p *Provider) signingKeys() (binaryarchive.Keys, error) {
	if p.archiveKeys != nil {
		return p.archiveKeys()
	}
	keys, err := p.sshKeys()
	if err != nil {
		return binaryarchive.Keys{}, err
	}
	return binaryarchive.LoadKeys(keys.Directory)
}

// sshKeys are dockhand's SSH keys (Keys).
func (p *Provider) sshKeys() (guestssh.Keys, error) {
	if p.Keys == "" {
		return guestssh.Keys{}, errors.New("tart: no folder for the SSH keys guests are reached with was given")
	}
	return guestssh.Keys{Directory: p.Keys}, nil
}

// launch copies the archive into the guest, checked by size and sha256,
// unpacks it, and starts the guest program under launchd, so it goes on
// if the connection drops.
func launch(ctx context.Context, g guest, archive string) error {
	const input = "/var/tmp/dockhand-check-input.tar"
	if err := g.Upload(ctx, archive, input, true); err != nil {
		return err
	}
	_, err := g.Command(ctx, nil, "sudo", "-n", "/bin/sh", "-c", `set -eu
[ ! -e "$2" ]
mkdir -m 755 "$2"
/usr/bin/tar xf "$1" -C "$2"
rm -f "$1"
exec /bin/launchctl bootstrap system "$2/guest.plist"`, "dockhand", input, guestRoot)
	return err
}

// stopped reports whether the guest program's launchd job has exited.
func stopped(ctx context.Context, g guest) (bool, error) {
	out, err := g.Command(ctx, nil, "sudo", "-n", "/bin/launchctl", "print", "system/"+guestLabel)
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "state = not running"), nil
}

// ReadsLinks says the guest reads what a passed build's binaries link
// (guest.tcl's links).
func (p *Provider) ReadsLinks() bool { return true }

// Supported says whether the provider can build on this Mac: Tart's
// macOS guests need Apple silicon (tartvm.Supported).
func Supported() bool { return tartvm.Supported() }
