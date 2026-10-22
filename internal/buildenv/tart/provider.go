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
	"github.com/herbygillot/dockhand/internal/buildenv/staging"
	"github.com/herbygillot/dockhand/internal/failpoint"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/binaryarchive"
	"github.com/herbygillot/dockhand/internal/macports/installation"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/guestssh"
)

// Protocol is the guest program's input and results format.
const Protocol = 1

// VerifierProtocol identifies how the guest program builds: part of an
// environment's origin, so raising it ends reuse of evidence the program
// recorded before (decision 28). A test pins guest.tcl, and fails until a
// change to it raises this or, for a change of wording only, re-pins.
const VerifierProtocol = 3

// guestRoot is where the guest program and the staged tree live in a clone.
const guestRoot = "/var/tmp/dockhand-check"

// guestLabel is the launchd job the guest program runs as.
const guestLabel = "org.dockhand.check"

//go:embed guest.tcl
var guestProgram []byte

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
	stager  func(ctx context.Context, job buildenv.Job, input guestInput, archive string) error
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

// guestInput is the guest program's input.
type guestInput struct {
	Protocol    int            `json:"protocol"`
	Run         string         `json:"run"`
	Attempt     int            `json:"attempt"`
	Prefix      string         `json:"prefix"`
	Platform    model.Platform `json:"platform"`
	Tests       string         `json:"tests"`
	TestTimeout int            `json:"test_timeout"`
	// BuildTimeout and LintTimeout are seconds, as TestTimeout is.
	BuildTimeout int           `json:"build_timeout"`
	LintTimeout  int           `json:"lint_timeout"`
	Targets      []guestTarget `json:"targets"`
	// Archives are the kept archives the guest installs targets from
	// rather than build them: in ArchiveSite, each in its port's
	// directory beside its signatures, which the keys at ArchiveKeys
	// verify. The guest program makes them an archive site of MacPorts'.
	Archives    []guestArchive `json:"archives,omitempty"`
	ArchiveSite string         `json:"archive_site,omitempty"`
	ArchiveKeys []string       `json:"archive_keys,omitempty"`
}

// guestArchive is a kept archive in the guest: the port it is of, and
// MacPorts' file name for it.
type guestArchive struct {
	Port string `json:"port"`
	Name string `json:"name"`
}

type guestTarget struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Portfile string          `json:"portfile"`
	Variants map[string]bool `json:"variants,omitempty"`
	// DependsOn are the targets this one needs, among this run's.
	DependsOn []string `json:"depends_on,omitempty"`
	// Blocked is a target whose changed dependency failed in an earlier
	// attempt: the guest records it blocked without building it.
	Blocked bool `json:"blocked,omitempty"`
	// Git is what a Git-fetched target's fetch must check out; absent for
	// a port fetched otherwise.
	Git *guestGit `json:"git,omitempty"`
	// Log is the name of its log, by its target (logName).
	Log string `json:"log"`
}

// guestGit is what a Git-fetched target's fetch must check out (batch 20):
// its ref, for the guest's words, and the commit the ref named when the
// check was planned, or the abbreviation that commit begins with. Expect
// is empty where the plan couldn't resolve the ref, and the guest only
// reports what the fetch checked out.
type guestGit struct {
	Ref    string `json:"ref"`
	Expect string `json:"expect,omitempty"`
}

// guestResults is what the guest program writes as it goes.
type guestResults struct {
	Protocol    int               `json:"protocol"`
	State       string            `json:"state"`
	Detail      string            `json:"detail"`
	Environment map[string]string `json:"environment"`
	// Building is the target the program is building, until its result
	// is written; empty between targets. Phase is the step of its build
	// under way, as lint, fetch, or install; empty where the guest program
	// says none, as before it said them.
	Building string `json:"building"`
	Phase    string `json:"phase"`
	// FromSource are the dependencies of the target building that have no
	// binary archive for the guest, which install builds from source.
	FromSource []string      `json:"from_source"`
	Targets    []guestResult `json:"targets"`
}

type guestResult struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	Phase   string `json:"phase"`
	Tests   string `json:"tests"`
	Log     string `json:"log"`
	Detail  string `json:"detail"`
	// Active are the ports that were active as the target built; absent
	// where the guest didn't record them, and empty where there were none.
	Active []guestPort `json:"active"`
	// Archive is the digest of the target's own archive, and ArchiveFile
	// where it is in the guest.
	Archive     string `json:"archive"`
	ArchiveFile string `json:"archive_file"`
	// Fetched is the commit a Git-fetched target's fetch checked out;
	// absent where the guest couldn't read it.
	Fetched string `json:"fetched,omitempty"`
	// Steps are where each step of the build began in its log, in the
	// order they ran; absent for a target not built.
	Steps []guestStep `json:"steps,omitempty"`
	// Transient is the line of a failed step's log that says the network
	// failed it upstream, as a module proxy's stream reset; the build is
	// tried again, as infrastructure.
	Transient string `json:"transient,omitempty"`
	// Dependency is the dependency whose failure failed the target at its
	// dependencies phase, as MacPorts named it, and DependencyVersion its
	// version and revision as the ports tree has them, 15.2.0_1; absent
	// where MacPorts' line didn't say, or the tree didn't.
	Dependency        string `json:"dependency,omitempty"`
	DependencyVersion string `json:"dependency_version,omitempty"`
}

// guestStep is where a step of a target's build began in its log: the
// line its output starts on, counting from 1.
type guestStep struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

// guestPort is a port active as a target built, as the guest saw it.
type guestPort struct {
	Name      string `json:"name"`
	Spec      string `json:"spec"`
	Directory string `json:"directory"`
	Archive   string `json:"archive"`
	// ArchiveFile is where its archive is in the guest; absent where the
	// guest kept none.
	ArchiveFile string `json:"archive_file,omitempty"`
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

	input := guestInput{Protocol: Protocol, Run: job.Run.Name(), Attempt: job.Execution.Attempt, Prefix: p.prefix(),
		Platform: job.Environment.Platform, Tests: string(job.Plan.Tests), TestTimeout: int(p.testTimeout() / time.Second),
		BuildTimeout: int(p.buildTimeout() / time.Second), LintTimeout: int(LintTimeout / time.Second)}
	if input.Tests == "" {
		input.Tests = string(model.TestsDeclared)
	}
	for _, target := range job.Targets {
		name := target.Target.Name
		if target.Target.Subport != "" {
			name = target.Target.Subport
		}
		t := guestTarget{ID: string(target.ID), Name: name, Portfile: target.Target.Portfile, Variants: target.Target.Variants, Log: buildenv.LogName(target.ID)}
		for _, dependency := range target.DependsOn {
			t.DependsOn = append(t.DependsOn, string(dependency))
		}
		if target.Git != nil {
			t.Git = &guestGit{Ref: target.Git.Ref, Expect: target.Git.Expected()}
		}
		_, t.Blocked = build.Blocked(target.ID)
		input.Targets = append(input.Targets, t)
	}
	// With nothing to build, no VM starts; the runner records what's
	// blocked.
	if !slices.ContainsFunc(input.Targets, func(t guestTarget) bool { return !t.Blocked }) {
		return nil
	}
	for _, archive := range job.Installs {
		if !binaryarchive.Installable(archive.Port, archive.Name) {
			return fmt.Errorf("an archive of %q named %q can't be installed", archive.Port, archive.Name)
		}
		input.Archives = append(input.Archives, guestArchive{Port: archive.Port, Name: archive.Name})
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
	build.Progress("building in " + vm)
	if err := launch(ctx, g, archive); err != nil {
		return p.trouble(ctx, "starting the build in "+vm, err)
	}
	return p.follow(ctx, g, started, job, build, release)
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

// LintTimeout bounds a target's lint, which takes seconds (D16).
const LintTimeout = 10 * time.Minute

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
			build.Progress(fmt.Sprintf("waiting for the Mac's VMs: %d are running, and macOS runs two at most", running))
			told = true
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(3 * p.poll()):
		}
	}
}

// stage packs the revision's tree, its port index for the release, and
// the guest program and its input into one archive.
func (p *Provider) stage(ctx context.Context, job buildenv.Job, input guestInput, archive string) error {
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
	payload := map[string][]byte{"guest.tcl": guestProgram, "input.json": data, "guest.plist": guestPlist(input.Prefix)}
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

// follow reads the guest's results as they grow, recording each target's
// as it appears with its log, until the program finishes. A program that
// stops without finishing, a VM that stops, or a guest that can't be read
// for a minute is infrastructure trouble; what was recorded stays, and the
// target the guest was building is recorded as interrupted.
func (p *Provider) follow(ctx context.Context, g guest, started run, job buildenv.Job, build buildenv.Build, release macos.Release) error {
	recorded := 0
	failures := 0
	observed := false
	// building is the target the guest said it was building when last
	// read, and said is the progress line last said of it, which is said
	// again only once it changes: at most a line a read, and one a step
	// (SQL-S4's coalescing).
	building, said := "", ""
	// A lost guest's partial log of what it was building is fetched where
	// it still answers, as when its program stopped or errored; a stopped
	// VM's isn't asked for.
	lost := func(err error) error {
		return interrupted(job, build, building, err, func(name string) string { return p.partialLog(ctx, g, job, name) })
	}
	// take takes in what the guest has written: what it reported about
	// itself, once, and each target's result not yet recorded. Every read
	// goes through it, the last one too, since the guest writes its
	// facts only with its first result.
	take := func(results guestResults) error {
		if !observed && results.Environment["macos"] != "" {
			observed = true
			if message := driftReport(release, job.Environment.DeveloperTools, results.Environment); message != "" {
				build.Progress(message)
			}
			if err := build.Observe(reported(results.Environment)); err != nil {
				return err
			}
		}
		for ; recorded < len(results.Targets); recorded++ {
			if err := p.record(ctx, g, job, build, results.Targets[recorded]); err != nil {
				return err
			}
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-started.Done():
			return interrupted(job, build, building, buildenv.Infrastructure("the VM stopped: %v", started.Err()), nil)
		case <-time.After(p.poll()):
		}
		results, err := p.read(ctx, g)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if failures++; time.Duration(failures)*p.poll() >= time.Minute {
				return lost(buildenv.Infrastructure("reading the guest's results: %w", err))
			}
			continue
		}
		failures = 0
		// The acceptance harness's X2 row, D-I7, makes a read fail once,
		// as a fault of dockhand's own or an error nothing classifies,
		// neither tried again, or as infrastructure, which is.
		switch failpoint.Fails("tart.results") {
		case "fault":
			return buildenv.ErrDockhandFault.Because("the guest's results were read wrong, as a failpoint asked")
		case "error":
			return errors.New("the guest's results were lost, as a failpoint asked")
		case "infrastructure":
			return lost(buildenv.Infrastructure("the guest stopped answering, as a failpoint asked"))
		}
		if results.Protocol != Protocol {
			return buildenv.ErrDockhandFault.Because("the guest program wrote protocol %d results; this dockhand reads %d", results.Protocol, Protocol)
		}
		if err := take(results); err != nil {
			return err
		}
		building = results.Building
		// What it's building has its log copied as it grows, for logs
		// --follow, in any process, where the log came back only when
		// the target ended (the gnome-voice session's notes, 2026-10-20).
		if building != "" {
			p.liveLog(ctx, g, job, model.TargetID(building))
		}
		if line := buildingWords(job, release, results); line != said {
			said = line
			if line != "" {
				build.Progress(line)
			}
		}
		switch results.State {
		case "finished":
			// A failure the network made upstream is the environment's, not
			// the port's: the build is tried again (dolt's go build on
			// proxy.golang.org's stream reset, field testing at 23c5e2aa).
			for _, target := range results.Targets {
				if target.Outcome == string(model.OutcomeFailed) && target.Transient != "" {
					return refusal.New(refusal.Network, "%s's build met a network fault upstream: %s", target.ID, target.Transient)
				}
			}
			return nil
		case "errored":
			return lost(buildenv.Infrastructure("the guest: %s", results.Detail))
		}
		if gone, err := stopped(ctx, g); err == nil && gone {
			// It may have written its last results after the read above.
			if last, err := p.read(ctx, g); err == nil && last.Protocol == Protocol {
				if err := take(last); err != nil || last.State == "finished" {
					return err
				}
				building = last.Building
			}
			log, _ := g.Read(ctx, guestRoot+"/runner.log", true)
			return lost(buildenv.Infrastructure("the guest program stopped before it finished: %s", strings.TrimSpace(tail(string(log), 400))))
		}
	}
}

// buildingWords is what the guest is building, and how far it is, as
// "building jq (2 of 5): fetch"; empty between targets.
func buildingWords(job buildenv.Job, release macos.Release, results guestResults) string {
	if results.Building == "" {
		return ""
	}
	name := results.Building
	if i := slices.IndexFunc(job.Targets, func(target buildenv.Target) bool { return string(target.ID) == results.Building }); i >= 0 {
		name = job.Targets[i].Target.Name
		if len(job.Targets) > 1 {
			name += fmt.Sprintf(" (%d of %d)", i+1, len(job.Targets))
		}
	}
	line := "building " + name
	if results.Phase != "" {
		line += ": " + results.Phase
	}
	// What builds from source takes the time, and is said before it does,
	// as "rust, cargo: no archive for macOS 27 arm64; building from source".
	if len(results.FromSource) > 0 {
		line += fmt.Sprintf("; %s: no archive for macOS %s %s; building from source", strings.Join(results.FromSource, ", "), release.Product, job.Environment.Platform.Architecture)
	}
	return line
}

// interrupted records the target a lost guest was building as
// interrupted, with why it was lost, and returns why. The runner tries it
// again, as it does a target the guest never reached; the result says the
// guest got as far as building it, with the log it had written, where
// fetch brings it: exiv2's check-448 kept an empty directory for its
// interrupted first attempt, and nothing said why (the exiv2 field notes,
// 2026-10-19).
func interrupted(job buildenv.Job, build buildenv.Build, building string, cause error, fetch func(name string) string) error {
	i := slices.IndexFunc(job.Targets, func(target buildenv.Target) bool { return string(target.ID) == building })
	if i < 0 {
		return cause
	}
	// What the environment's trouble was, without the refusal's own
	// words around it.
	detail := cause.Error()
	if r := refusal.Of(cause); r != nil && errors.Unwrap(r) != nil {
		detail = errors.Unwrap(r).Error()
	}
	result := model.TargetResult{Target: job.Targets[i].ID, Outcome: model.OutcomeInterrupted, Detail: detail}
	if fetch != nil {
		// The guest names each target's log by its target.
		result.Log = fetch(buildenv.LogName(job.Targets[i].ID))
	}
	if err := build.Record(buildenv.Reported(result)); err != nil {
		return fmt.Errorf("%w; %s wasn't recorded as interrupted: %v", cause, building, err)
	}
	return cause
}

func (p *Provider) read(ctx context.Context, g guest) (guestResults, error) {
	var results guestResults
	data, err := g.Read(ctx, guestRoot+"/results.json", true)
	if err != nil {
		return results, err
	}
	return results, json.Unmarshal(data, &results)
}

// stopped reports whether the guest program's launchd job has exited.
func stopped(ctx context.Context, g guest) (bool, error) {
	out, err := g.Command(ctx, nil, "sudo", "-n", "/bin/launchctl", "print", "system/"+guestLabel)
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "state = not running"), nil
}

// record copies a target's log out of the guest and records its result.
// partialLog fetches a log the guest was still writing into the job's
// directory, within half a minute, and says where it is; empty where the
// guest didn't give it.
func (p *Provider) partialLog(ctx context.Context, g guest, job buildenv.Job, name string) string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	local := filepath.Join(job.Directory, name)
	if err := g.Download(ctx, guestRoot+"/"+name, local, true); err != nil {
		return ""
	}
	return local
}

// liveLog copies what the log of the target building has grown by since
// the last read into the job's directory, by its name (buildenv.LogName),
// within the poll's time; a copy that can't be made waits for the next
// read. The finished log's Download replaces it.
func (p *Provider) liveLog(ctx context.Context, g guest, job buildenv.Job, id model.TargetID) {
	if job.Directory == "" || os.MkdirAll(job.Directory, 0o755) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, max(p.poll(), 5*time.Second))
	defer cancel()
	name := buildenv.LogName(id)
	_ = g.Append(ctx, guestRoot+"/"+name, filepath.Join(job.Directory, name), true)
}

func (p *Provider) record(ctx context.Context, g guest, job buildenv.Job, build buildenv.Build, got guestResult) error {
	var target buildenv.Target
	found := false
	for _, t := range job.Targets {
		if string(t.ID) == got.ID {
			target, found = t, true
		}
	}
	if !found {
		return buildenv.ErrDockhandFault.Because("the guest reported %q, which this job didn't ask for", got.ID)
	}
	result := model.TargetResult{Target: target.ID, Outcome: model.Outcome(got.Outcome), Phase: model.Phase(got.Phase), Tests: model.TestOutcome(got.Tests)}
	switch result.Outcome {
	case model.OutcomePassed, model.OutcomeFailed, model.OutcomeBlocked:
	default:
		return buildenv.ErrDockhandFault.Because("the guest reported %s %q", got.ID, got.Outcome)
	}
	if result.Outcome != model.OutcomeFailed {
		result.Phase = ""
	}
	if result.Phase == model.PhaseDependencies {
		result.Dependency = model.FailedDependency{Name: got.Dependency, Version: got.DependencyVersion}
	}
	if result.Tests == "" {
		result.Tests = model.TestsNone
	}
	if got.Log != "" && filepath.Base(got.Log) == got.Log {
		local := filepath.Join(job.Directory, got.Log)
		if err := g.Download(ctx, guestRoot+"/"+got.Log, local, true); err == nil {
			result.Log = local
			// Where each step began is a place in this log, kept with it.
			for _, step := range got.Steps {
				result.Steps = append(result.Steps, model.LogStep{Name: model.Step(step.Name), Line: step.Line})
			}
		}
	}
	report := buildenv.TargetReport{Fetched: got.Fetched}
	if got.Fetched != "" && !git.ValidObjectID(got.Fetched) {
		return buildenv.ErrDockhandFault.Because("the guest reported %s fetched %q, which isn't a commit", got.ID, got.Fetched)
	}
	if got.Active != nil {
		report.Active = make([]model.ActivePort, len(got.Active))
		for i, port := range got.Active {
			report.Active[i] = model.ActivePort{Name: port.Name, Spec: port.Spec, Directory: port.Directory, Archive: port.Archive}
		}
	}
	result.Archive, result.Detail = got.Archive, got.Detail
	report.Result = result
	if err := build.Record(report); err != nil {
		return err
	}
	if got.Detail != "" {
		build.Progress(got.ID + ": " + got.Detail)
	}
	// A passed target's archive is kept, for a later build of a target
	// that needs it to install rather than build it again (decision 28).
	// One that can't be kept leaves the result as it is.
	if result.Outcome == model.OutcomePassed && got.Archive != "" {
		if err := p.keep(ctx, g, build, target.ID, got.ArchiveFile); err != nil {
			build.Progress(fmt.Sprintf("%s: its archive wasn't kept: %v", got.ID, err))
		}
	}
	// The archives its dependencies were installed from are kept too, for
	// a later guest to install them rather than build them again: rust
	// and cargo, built from source where MacPorts had no archive for the
	// release yet, took hours of a check on macOS 27 (the M1's run at
	// d302e744, batch 90). One that isn't kept is said, and builds again
	// next time.
	for _, port := range got.Active {
		if port.Archive == "" || port.ArchiveFile == "" || isTarget(job, port.Name) {
			continue
		}
		active := model.ActivePort{Name: port.Name, Spec: port.Spec, Directory: port.Directory, Archive: port.Archive}
		if err := p.keepDependency(ctx, g, build, active, port.ArchiveFile); err != nil {
			build.Progress(fmt.Sprintf("%s: the archive of its dependency %s wasn't kept: %v", got.ID, port.Name, err))
		}
	}
	return nil
}

// isTarget reports whether a port is one of the plan's targets, built in
// this job or not, whose archives are their results' and aren't kept as
// dependencies'.
func isTarget(job buildenv.Job, port string) bool {
	targets := slices.Clone(job.Plan.Targets)
	for _, target := range job.Targets {
		targets = append(targets, target.PlanTarget)
	}
	return slices.ContainsFunc(targets, func(target model.PlanTarget) bool {
		return strings.EqualFold(target.Target.Name, port) || strings.EqualFold(target.Target.Subport, port)
	})
}

// keep fetches a target's archive from where the guest's MacPorts keeps
// it, its prefix's software directory, into the build's store.
func (p *Provider) keep(ctx context.Context, g guest, build buildenv.Build, target model.TargetID, file string) error {
	software := path.Join(p.prefix(), "var/macports/software") + "/"
	if file == "" || path.Clean(file) != file || !strings.HasPrefix(file, software) {
		return fmt.Errorf("the guest named %q, not a file in %s", file, software)
	}
	return build.Keep(target, path.Base(file), func(local string) error { return g.Download(ctx, file, local, true) })
}

// keepDependency fetches the archive a dependency was installed from,
// from the guest's software directory, as keep does a target's.
func (p *Provider) keepDependency(ctx context.Context, g guest, build buildenv.Build, port model.ActivePort, file string) error {
	software := path.Join(p.prefix(), "var/macports/software") + "/"
	if path.Clean(file) != file || !strings.HasPrefix(file, software) {
		return fmt.Errorf("the guest named %q, not a file in %s", file, software)
	}
	return build.KeepDependency(port, path.Base(file), func(local string) error { return g.Download(ctx, file, local, true) })
}

// reported is what the guest reported about itself, for the pull request's
// Tested on.
func reported(environment map[string]string) model.Observed {
	// The guest records port version's own words, "Version: 2.12.6".
	macports, _ := installation.ParseVersion([]byte(environment["macports"]))
	return model.Observed{MacOS: environment["macos"], Build: environment["build"], Architecture: environment["architecture"],
		Xcode: environment["xcode"], XcodeBuild: environment["xcode_build"], Tools: environment["tools"],
		DeveloperDir: environment["developer_dir"], MacPorts: macports}
}

// driftReport compares the guest's tools with the facts table's row for
// its release and profile (decision 10), which is what its plan was read
// with: a difference is reported, never judged. An image with Xcode is
// compared with the Xcode row, Xcode's version as well.
func driftReport(release macos.Release, developer model.DeveloperTools, environment map[string]string) string {
	profile := macos.ProfileTools
	if developer == model.DeveloperToolsXcode {
		profile = macos.ProfileXcode
	}
	facts, ok := macos.Table().Lookup(release.Darwin, "arm64", profile)
	if !ok {
		return ""
	}
	var differences []string
	if xcode := environment["xcode"]; profile == macos.ProfileXcode && xcode != "" && xcode != facts.Xcode {
		differences = append(differences, fmt.Sprintf("Xcode %s; the facts table has %s", xcode, facts.Xcode))
	}
	if tools := environment["tools"]; tools != "" && tools != facts.Tools {
		differences = append(differences, fmt.Sprintf("Command Line Tools %s; the facts table has %s", tools, facts.Tools))
	}
	if len(differences) == 0 {
		return ""
	}
	return fmt.Sprintf("drift: the guest has %s, from %s on %s", strings.Join(differences, ", and "), facts.Source.From, facts.Source.Date)
}

func tail(text string, n int) string {
	if len(text) > n {
		return "…" + text[len(text)-n:]
	}
	return text
}

// Supported says whether the provider can build on this Mac: Tart's
// macOS guests need Apple silicon (tartvm.Supported).
func Supported() bool { return tartvm.Supported() }
