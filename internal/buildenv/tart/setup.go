package tart

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/installation"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/subprocess"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/tart/provision"
)

// Status is what dockhand has set up for Tart: the releases with a base
// image, those with an Xcode image too, and this Mac's own release.
type Status struct {
	Base  []macos.Release
	Xcode []macos.Release
	Host  macos.Release
	// Restorable are the releases whose base image is gone and whose
	// golden copy is kept, which setup tart restores in about a minute
	// (Codex's review, checked against a268b6e5: setup said "not set up"
	// and offered a 60 GB download).
	Restorable []macos.Release
	// Behind are the images whose MacPorts is older than this Mac's, which
	// reads the ports they build.
	Behind []BehindImage
	// MacPorts are the MacPorts each image was made with, as its record
	// says, by its name; an image with no record has none.
	MacPorts map[string]string
}

// CheckImage is whether a release's checks build in its Xcode image,
// which they do wherever it has one, else in its base image; ok is false
// where it has neither.
func (s Status) CheckImage(release macos.Release) (xcode, ok bool) {
	has := func(releases []macos.Release) bool {
		return slices.ContainsFunc(releases, func(one macos.Release) bool { return one.Darwin == release.Darwin })
	}
	switch {
	case has(s.Xcode):
		return true, true
	case has(s.Base):
		return false, true
	}
	return false, false
}

// ImageMacPorts is the MacPorts a release's base or Xcode image was made
// with, as its record says, or empty.
func (s Status) ImageMacPorts(release macos.Release, xcode bool) string {
	if xcode {
		return s.MacPorts[xcodeImage(release)]
	}
	return s.MacPorts[baseImage(release)]
}

// BehindOf is the release's base or Xcode image, where it's behind this
// Mac's MacPorts.
func (s Status) BehindOf(release macos.Release, xcode bool) (BehindImage, bool) {
	for _, image := range s.Behind {
		if image.Release.Darwin == release.Darwin && image.Xcode == xcode {
			return image, true
		}
	}
	return BehindImage{}, false
}

// BehindImage is an image with an older MacPorts than this Mac's: its
// release, whether it's the Xcode image, and the two versions.
type BehindImage struct {
	Release        macos.Release
	Xcode          bool
	MacPorts, Host string
}

// HasHostImage reports whether there's a base image of this Mac's own
// macOS, which setup offers to make where there isn't.
func (s Status) HasHostImage() bool {
	return slices.ContainsFunc(s.Base, func(release macos.Release) bool { return release.Darwin == s.Host.Darwin })
}

// HostRestorable reports whether this Mac's macOS has no base image but a
// golden copy, which setup restores rather than makes.
func (s Status) HostRestorable() bool {
	return slices.ContainsFunc(s.Restorable, func(release macos.Release) bool { return release.Darwin == s.Host.Darwin })
}

// Readiness is what dockhand's Tart images let a check do on this Mac.
type Readiness int

const (
	// NoImage is no base image: a check has nowhere to build.
	NoImage Readiness = iota
	// XcodeOnly is an Xcode image made without the base image a check
	// needs beside it (the rc1 full stage's A2).
	XcodeOnly
	// NotForHost is base images, none of them this Mac's macOS.
	NotForHost
	// Ready is a base image of this Mac's macOS, or of a release this Mac
	// isn't known to run.
	Ready
)

// Readiness says what the images let a check do; Tart that isn't installed,
// or a Mac that can't run it, is before there's a Status to ask.
func (s Status) Readiness() Readiness {
	switch {
	case len(s.Base) == 0 && len(s.Xcode) > 0:
		return XcodeOnly
	case len(s.Base) == 0:
		return NoImage
	case s.Host.Product != "" && !s.HasHostImage():
		return NotForHost
	}
	return Ready
}

// Status lists dockhand's images by release.
func (p *Provider) Status(ctx context.Context) (Status, error) {
	m, err := p.vms()
	if err != nil {
		return Status{}, err
	}
	images, err := m.Images(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("listing dockhand's Tart images: %w", err)
	}
	var status Status
	runtime, resolveErr := p.Tart.Resolve()
	behind := func(release macos.Release, name string, xcode bool) {
		if resolveErr != nil {
			return
		}
		if manifest, found, err := tartvm.ReadImageRecord(runtime.Home, name); err == nil && found {
			if manifest.MacPortsVersion != "" {
				if status.MacPorts == nil {
					status.MacPorts = map[string]string{}
				}
				status.MacPorts[name] = manifest.MacPortsVersion
			}
			if version, host := p.behind(ctx, manifest.MacPortsVersion); version != "" {
				status.Behind = append(status.Behind, BehindImage{Release: release, Xcode: xcode, MacPorts: version, Host: host})
			}
		}
	}
	for _, release := range macos.Known() {
		if slices.Contains(images, baseImage(release)) {
			status.Base = append(status.Base, release)
			behind(release, baseImage(release), false)
		} else if slices.Contains(images, tartvm.GoldenName(baseImage(release))) {
			status.Restorable = append(status.Restorable, release)
		}
		if slices.Contains(images, xcodeImage(release)) {
			status.Xcode = append(status.Xcode, release)
			behind(release, xcodeImage(release), true)
		}
	}
	if darwin, err := p.hostRelease(); err == nil {
		status.Host, _ = macos.ReleaseForDarwin(darwin)
	}
	return status, nil
}

// xcodeImage is the image setup makes for a release with Xcode as well.
func xcodeImage(release macos.Release) string {
	return tartvm.Prepared{Release: release, Profile: macos.ProfileXcode}.Name()
}

// SetupDisk is the most disk a release's base image takes to make: the
// image, up to 29 GB for Tahoe, and the vanilla image it starts from, 28 GB.
// The golden copy shares the image's blocks.
const SetupDisk = "60 GB"

// XcodeDisk is the same for an Xcode image, up to 34 GB for Monterey's,
// and the vanilla image when the base image didn't download it first.
const XcodeDisk = "65 GB"

// SetupOptions choose what setup makes.
type SetupOptions struct {
	// Release is the macOS release, by name or product version; this
	// Mac's when empty.
	Release string
	// Check validates an existing image in a disposable clone, making
	// nothing; Rebuild makes a replacement even when the image exists.
	Check, Rebuild bool
	// MacPortsVersion is the MacPorts the image installs; DefaultMacPorts
	// when empty.
	MacPortsVersion string
	// Xcode is an Xcode .xip, or a folder of them, for the release's Xcode
	// image, an add-on beside its base image.
	Xcode string
	// Xcodes are the configuration's Xcode for each release it names
	// (providers.tart.xcode), by release name or number. A release it
	// doesn't name gets what MacPorts' arm64 builder for it runs.
	Xcodes map[string]string
}

// DefaultMacPorts is the oldest MacPorts an image installs unless asked
// for another; one made installs this Mac's where it's newer
// (newMacPorts).
const DefaultMacPorts = macports.DefaultBaseVersion

// newMacPorts is the MacPorts an image made now installs: this Mac's,
// where it's newer than DefaultMacPorts, else DefaultMacPorts.
func (p *Provider) newMacPorts(ctx context.Context) string {
	host := p.macPorts
	if host == "" {
		host = HostMacPorts(ctx, macports.FindTclsh())
	}
	if host != "" && !macports.BaseAtLeast(DefaultMacPorts, host) {
		return host
	}
	return DefaultMacPorts
}

// SetupResult is the image setup made, or found ready.
type SetupResult struct {
	Image            string
	Release          macos.Release
	MacPorts         string
	CommandLineTools string
	// Xcode is the Xcode image's Xcode version; empty for a base image.
	Xcode string
	// Reused is an image that was already there and passed its checks.
	Reused bool
	// HostMacPorts is the MacPorts this Mac reads ports with, where it
	// isn't the image's, which builds them: the two can differ on what a
	// Portfile means. Empty where they're the same, or this Mac's can't
	// be told.
	HostMacPorts string
}

// Setup makes a release's base image with dockhand's provisioner: from
// Cirrus Labs' vanilla macOS image, with the Command Line Tools of the
// release's pinned generation and MacPorts. With an Xcode archive it makes
// the release's Xcode image instead, the same with Xcode too. It keeps a
// golden copy that a lost image is restored from. An image that exists is checked in a
// disposable clone instead, and left as it is. Progress is written to
// progress as it goes.
func (p *Provider) Setup(ctx context.Context, options SetupOptions, progress io.Writer) (SetupResult, error) {
	if options.Check && options.Rebuild {
		return SetupResult{}, errors.New("--check and --rebuild cannot be used together; choose one")
	}
	release, err := p.release(options.Release)
	if err != nil {
		return SetupResult{}, err
	}
	// Making an image is the costly case, and it's named before it
	// starts: an image that exists, or its golden copy, costs a check.
	if !options.Check {
		m, err := p.vms()
		if err != nil {
			return SetupResult{}, err
		}
		images, err := m.Images(ctx)
		if err != nil {
			return SetupResult{}, fmt.Errorf("listing dockhand's Tart images: %w", err)
		}
		has := func(image string) bool {
			return slices.Contains(images, image) || slices.Contains(images, tartvm.GoldenName(image))
		}
		// An Xcode image is an add-on beside the release's base image,
		// and a check takes the release only where the base image is
		// (Environments): made alone, it spent 65 GB on an image no check
		// could use (the rc3 full run, 2026-10-06).
		if options.Xcode != "" && !has(baseImage(release)) {
			return SetupResult{}, fmt.Errorf("the base image for macOS %s (%s) is missing; run dockhand setup tart %s first, then rerun with --xcode",
				release.Product, release.Name, release.Slug)
		}
		image, disk := baseImage(release), SetupDisk
		if options.Xcode != "" {
			image, disk = xcodeImage(release), XcodeDisk
		}
		making := options.Rebuild || !has(image)
		// An image made now installs this Mac's MacPorts, where it's newer
		// than the floor, so the image builds with what reads its ports:
		// a hard 2.12.6 left the rebuild a stale image's hint named as
		// stale as before (field testing, 2026-10-16). One that exists is
		// checked against the floor, as before.
		if making && options.MacPortsVersion == "" {
			options.MacPortsVersion = p.newMacPorts(ctx)
		}
		if progress != nil && making {
			fmt.Fprintf(progress, "Creating %s for macOS %s (%s); requires up to %s of disk space.\n", image, release.Product, release.Name, disk)
		}
	}
	config, err := p.provisionConfig(release, options)
	if err != nil {
		return SetupResult{}, err
	}
	provisioner := provision.Provisioner{Progress: progress, Config: config}
	result, err := provisioner.Run(ctx, provision.Options{Check: options.Check, Rebuild: options.Rebuild})
	if err != nil {
		return SetupResult{}, err
	}
	made := SetupResult{Image: result.Image, Release: release, MacPorts: result.MacPortsVersion,
		CommandLineTools: result.CommandLineTools, Xcode: result.XcodeVersion, Reused: result.Reused}
	if host := HostMacPorts(ctx, macports.FindTclsh()); host != result.MacPortsVersion {
		made.HostMacPorts = host
	}
	return made, nil
}

// provisionConfig is what the provisioner makes for a release: its base
// image, or with an Xcode archive its Xcode image, with the Xcode XcodeFor
// chooses. A base image has no Xcode.
func (p *Provider) provisionConfig(release macos.Release, options SetupOptions) (provision.Config, error) {
	config := provision.Config{
		Executable: p.Tart.Executable, Home: p.Tart.Home, MacPortsVersion: options.MacPortsVersion, Xcode: options.Xcode,
		Platform: model.Platform{OS: "darwin", Version: strconv.Itoa(release.Darwin), Architecture: "arm64"},
	}
	if options.Xcode == "" {
		return config, nil
	}
	xcode, err := XcodeFor(release, options.Xcodes)
	config.XcodeVersion = xcode
	return config, err
}

// MissingXcode is an Xcode a release's image needs whose archive setup
// wasn't given.
type MissingXcode = macos.MissingXcode

// Xcodes is where the xcodes command is, when it's installed: it downloads
// Xcode from Apple with the person's Apple ID, which dockhand never sees.
// It signs in through Apple's own sign-in, which Apple doesn't document
// for other programs: an exception to depending only on documented
// interfaces, which the person chose (docs/roadmap.md).
func (p *Provider) Xcodes() (string, bool) {
	if p.xcodes != "" {
		return p.xcodes, true
	}
	path, err := exec.LookPath("xcodes")
	return path, err == nil
}

// ErrXcodes is xcodes failing to download an Xcode, which says why in its
// own output: most often, that it needs the person to sign in.
var ErrXcodes = errors.New("xcodes could not download Xcode")

// DownloadXcode downloads the Xcode setup is missing with xcodes, into the
// folder setup looked in, where setup finds it and checks it is Apple's
// before taking it. At a terminal, the person answers xcodes' sign-in there;
// without one, xcodes uses the sign-in it keeps, and fails at once when it
// has none.
func (p *Provider) DownloadXcode(ctx context.Context, missing *MissingXcode, in io.Reader, out, errs io.Writer) (string, error) {
	xcodes, ok := p.Xcodes()
	switch {
	case !ok:
		return "", errors.New("xcodes isn't installed: sudo port install xcodes")
	case missing.Folder == "":
		return "", fmt.Errorf("%s is a file; choose a download directory for Xcode %s", missing.Path, missing.Version)
	}
	command := exec.CommandContext(ctx, xcodes, "download", missing.Version, "--directory", missing.Folder)
	command.Stdin, command.Stdout, command.Stderr = in, out, errs
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("%w: xcodes download %s: %w", ErrXcodes, missing.Version, err)
	}
	path, _, err := macos.SelectXcode(missing.Folder, missing.Release, missing.Version)
	if err != nil {
		return "", fmt.Errorf("xcodes finished, but no suitable Xcode archive was found: %w", err)
	}
	return path, nil
}

// XcodeFor is the Xcode a release's Xcode image installs: the one the
// configuration names for it (providers.tart.xcode), else what MacPorts'
// arm64 builder for the release runs (macos.Release.Xcode). A name the
// configuration uses that is no release is refused, rather than ignored.
func XcodeFor(release macos.Release, configured map[string]string) (string, error) {
	xcode, err := xcodeFor(release, configured)
	return xcode.Version, err
}

// ReleaseXcode is the Xcode a release's Xcode image installs, and whether
// the configuration named it rather than MacPorts' builder.
type ReleaseXcode struct {
	Release    macos.Release
	Version    string
	Configured bool
}

// Xcodes is each release's Xcode, as XcodeFor chooses it, oldest release
// first.
func Xcodes(configured map[string]string) ([]ReleaseXcode, error) {
	var xcodes []ReleaseXcode
	for _, release := range macos.Known() {
		xcode, err := xcodeFor(release, configured)
		if err != nil {
			return nil, err
		}
		xcodes = append(xcodes, xcode)
	}
	return xcodes, nil
}

func xcodeFor(release macos.Release, configured map[string]string) (ReleaseXcode, error) {
	xcode := ReleaseXcode{Release: release, Version: release.Xcode}
	for name, value := range configured {
		named, err := macos.ParseRelease(name)
		if err != nil {
			return ReleaseXcode{}, fmt.Errorf("providers.tart.xcode: %w", err)
		}
		if named.Darwin == release.Darwin {
			xcode.Version, xcode.Configured = value, true
		}
	}
	return xcode, nil
}

// release is the release named, by name or product version, or this Mac's
// when none is.
func (p *Provider) release(name string) (macos.Release, error) {
	if name != "" {
		return macos.ParseRelease(name)
	}
	darwin, err := p.hostRelease()
	if err != nil {
		return macos.Release{}, err
	}
	release, err := macos.ReleaseForDarwin(darwin)
	if err != nil {
		return macos.Release{}, fmt.Errorf("this Mac's macOS: %w", err)
	}
	return release, nil
}

// HostMacPorts is the version of the MacPorts whose port-tclsh evaluates
// ports on this Mac, or empty when it can't be read.
func HostMacPorts(ctx context.Context, tclsh string) string {
	if tclsh == "" {
		return ""
	}
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "port", Path: filepath.Join(filepath.Dir(tclsh), "port"), Args: []string{"version"}, Limit: 1 << 16})
	if err != nil {
		return ""
	}
	version, _ := installation.ParseVersion(result.Output)
	return version
}
