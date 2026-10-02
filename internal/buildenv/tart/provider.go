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
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/binaryarchive"
	"github.com/herbygillot/dockhand/internal/macports/installation"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/channel"
)

// Protocol is the guest program's input and results format.
const Protocol = 1

// VerifierProtocol identifies how the guest program builds: part of an
// environment's origin, so raising it ends reuse of evidence the program
// recorded before (decision 28). A test pins guest.tcl, and fails until a
// change to it raises this or, for a change of wording only, re-pins.
const VerifierProtocol = 2

// guestRoot is where the guest program and the staged tree live in a clone.
const guestRoot = "/var/tmp/dockhand-check"

// guestLabel is the launchd job the guest program runs as.
const guestLabel = "org.dockhand.check"

//go:embed guest.tcl
var guestProgram []byte

// Provider builds on dockhand's Tart images.
type Provider struct {
	// Tart is dockhand's Tart installation and home.
	Tart tartvm.Client
	Repo *git.Repository
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
	keys, err := channel.DefaultKeys()
	if err != nil {
		return nil, err
	}
	p.machine = newNative(client, keys)
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
			return nil, errors.New("--on tart:all: dockhand has no Tart images yet; dockhand providers setup tart makes one for this Mac's macOS")
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
			return nil, fmt.Errorf("no Tart image for macOS %s (%s): dockhand providers setup tart %s makes %s", release.Product, release.Name, release.Slug, baseImage(release))
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
	if err != nil || unmet.Needs != model.RequiresXcode {
		return ""
	}
	return fmt.Sprintf("dockhand providers setup tart %s --xcode <Xcode .xip, or a folder of them> makes macOS %s's Xcode image", release.Slug, release.Product)
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
	var clones []buildenv.Leftover
	for _, name := range images {
		if strings.HasPrefix(name, clonePrefixAll) {
			clones = append(clones, buildenv.Leftover{Ref: name, What: "Tart clone " + name})
		}
	}
	return clones, nil
}

// RemoveLeftover stops and deletes one check clone. Any other name is
// refused, so no image a check clones from is ever touched.
func (p *Provider) RemoveLeftover(ctx context.Context, ref string) error {
	if !strings.HasPrefix(ref, clonePrefixAll) {
		return fmt.Errorf("%s is not a check's clone; only those are removed", ref)
	}
	m, err := p.vms()
	if err != nil {
		return err
	}
	if err := m.Stop(ctx, ref); err != nil {
		return err
	}
	return m.Delete(ctx, ref)
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
	Targets     []guestResult     `json:"targets"`
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
}

// Execute builds the job's targets in a fresh clone of the release's image.
// Trouble with the VM or the guest is an infrastructure error, which the
// runner tries again; a target that fails to build is a result.
func (p *Provider) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) (err error) {
	m, err := p.vms()
	if err != nil {
		return fmt.Errorf("%w: %w", buildenv.ErrInfrastructure, err)
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
		t := guestTarget{ID: string(target.ID), Name: name, Portfile: target.Target.Portfile, Variants: target.Target.Variants}
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
			if deleteErr := m.Delete(cleanup, vm); deleteErr != nil && err == nil {
				build.Progress("deleting " + vm + ": " + deleteErr.Error())
			}
		}()
	}
	if err != nil {
		return err
	}
	defer func() { _ = started.Stop(cleanup, time.Minute) }()
	g, err := m.Reach(ctx, vm, image)
	if err != nil {
		return p.trouble(ctx, "reaching "+vm, err)
	}
	defer g.Close(cleanup)
	if err := await(ctx, g, started, 4*time.Minute); err != nil {
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
	return 30 * time.Minute
}

// BuildTimeout is a target's build's bound where the configuration sets
// none: GitHub's cap on a job, which a build that's run that long would
// meet there too (D16).
const BuildTimeout = 6 * time.Hour

// LintTimeout bounds a target's lint, which takes seconds (D16).
const LintTimeout = 10 * time.Minute

func (p *Provider) buildTimeout() time.Duration {
	if p.BuildTimeout > 0 {
		return p.BuildTimeout
	}
	return BuildTimeout
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
	return fmt.Errorf("%w: %s: %w", buildenv.ErrInfrastructure, doing, err)
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
		return false, nil, fmt.Errorf("%w: cloning %s: %w", buildenv.ErrInfrastructure, image, err)
	}
	if started, err = m.Start(vm); err != nil {
		return true, nil, fmt.Errorf("%w: starting %s: %w", buildenv.ErrInfrastructure, vm, err)
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
		build.Progress(fmt.Sprintf("giving the guest %s, from the archive kept of its build", archive.Target))
		entry, err := binaryarchive.Sign(ctx, keys, binaryarchive.Archive{Port: archive.Port, Name: archive.Name, Digest: archive.Digest, Path: archive.Path}, job.Directory)
		if err != nil {
			return fmt.Errorf("the archive kept of %s: %w", archive.Target, err)
		}
		for _, name := range slices.Sorted(maps.Keys(entry.Files)) {
			if err == nil {
				err = g.Upload(ctx, entry.Files[name], binaryarchive.EntryPath(archiveSite, entry.Port, name), true)
			}
			if local := entry.Files[name]; local != archive.Path {
				os.Remove(local)
			}
		}
		if err != nil {
			return err
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

// signingKeys are the keys archives given to guests are signed with, kept
// beside dockhand's SSH keys, where they have always been.
func (p *Provider) signingKeys() (binaryarchive.Keys, error) {
	if p.archiveKeys != nil {
		return p.archiveKeys()
	}
	keys, err := channel.DefaultKeys()
	if err != nil {
		return binaryarchive.Keys{}, err
	}
	return binaryarchive.LoadKeys(keys.Directory)
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
// for a minute is infrastructure trouble; what was recorded stays.
func (p *Provider) follow(ctx context.Context, g guest, started run, job buildenv.Job, build buildenv.Build, release macos.Release) error {
	recorded := 0
	failures := 0
	observed := false
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
			return fmt.Errorf("%w: the VM stopped: %v", buildenv.ErrInfrastructure, started.Err())
		case <-time.After(p.poll()):
		}
		results, err := p.read(ctx, g)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if failures++; time.Duration(failures)*p.poll() >= time.Minute {
				return fmt.Errorf("%w: reading the guest's results: %w", buildenv.ErrInfrastructure, err)
			}
			continue
		}
		failures = 0
		if results.Protocol != Protocol {
			return fmt.Errorf("%w: the guest program wrote protocol %d results; this dockhand reads %d", buildenv.ErrInfrastructure, results.Protocol, Protocol)
		}
		if err := take(results); err != nil {
			return err
		}
		switch results.State {
		case "finished":
			return nil
		case "errored":
			return fmt.Errorf("%w: the guest: %s", buildenv.ErrInfrastructure, results.Detail)
		}
		if gone, err := stopped(ctx, g); err == nil && gone {
			// It may have written its last results after the read above.
			if last, err := p.read(ctx, g); err == nil && last.State == "finished" {
				return take(last)
			}
			log, _ := g.Read(ctx, guestRoot+"/runner.log", true)
			return fmt.Errorf("%w: the guest program stopped before it finished: %s", buildenv.ErrInfrastructure, strings.TrimSpace(tail(string(log), 400)))
		}
	}
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
func (p *Provider) record(ctx context.Context, g guest, job buildenv.Job, build buildenv.Build, got guestResult) error {
	var target buildenv.Target
	found := false
	for _, t := range job.Targets {
		if string(t.ID) == got.ID {
			target, found = t, true
		}
	}
	if !found {
		return fmt.Errorf("%w: the guest reported %q, which this job didn't ask for", buildenv.ErrInfrastructure, got.ID)
	}
	result := model.TargetResult{Target: target.ID, Outcome: model.Outcome(got.Outcome), Phase: model.Phase(got.Phase), Tests: model.TestOutcome(got.Tests)}
	switch result.Outcome {
	case model.OutcomePassed, model.OutcomeFailed, model.OutcomeBlocked:
	default:
		return fmt.Errorf("%w: the guest reported %s %q", buildenv.ErrInfrastructure, got.ID, got.Outcome)
	}
	if result.Outcome != model.OutcomeFailed {
		result.Phase = ""
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
	if got.Fetched != "" {
		if !git.ValidObjectID(got.Fetched) {
			return fmt.Errorf("%w: the guest reported %s fetched %q, which isn't a commit", buildenv.ErrInfrastructure, got.ID, got.Fetched)
		}
		build.Fetched(target.ID, got.Fetched)
	}
	if got.Active != nil {
		active := make([]model.ActivePort, len(got.Active))
		for i, port := range got.Active {
			active[i] = model.ActivePort{Name: port.Name, Spec: port.Spec, Directory: port.Directory, Archive: port.Archive}
		}
		build.Consumed(target.ID, active)
	}
	result.Archive, result.Detail = got.Archive, got.Detail
	if err := build.Record(result); err != nil {
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
	return nil
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
