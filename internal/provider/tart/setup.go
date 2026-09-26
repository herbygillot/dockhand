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
	"github.com/herbygillot/dockhand/internal/tart/provision"
)

// Status is what dockhand has set up for Tart: the releases with a base
// image, those with an Xcode image too, and this Mac's own release.
type Status struct {
	Base  []macos.Release
	Xcode []macos.Release
	Host  macos.Release
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
	for _, release := range macos.Known() {
		if slices.Contains(images, baseImage(release)) {
			status.Base = append(status.Base, release)
		}
		if slices.Contains(images, xcodeImage(release)) {
			status.Xcode = append(status.Xcode, release)
		}
	}
	if darwin, err := p.hostRelease(); err == nil {
		status.Host, _ = macos.ReleaseForDarwin(darwin)
	}
	return status, nil
}

// xcodeImage is the image setup makes for a release with Xcode as well.
func xcodeImage(release macos.Release) string { return "dockhand-xcode-" + release.Slug }

// goldenImage is the copy setup keeps of a release's base image, and
// restores it from.
func goldenImage(release macos.Release) string { return "dockhand-golden-" + release.Slug }

// SetupDisk is the most disk a release's image takes to make: the image,
// up to 29 GB for Tahoe, and the vanilla image it starts from, 28 GB. The
// golden copy shares the image's blocks.
const SetupDisk = "60 GB"

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
}

// DefaultMacPorts is the MacPorts an image installs unless asked for
// another.
const DefaultMacPorts = macports.DefaultBaseVersion

// SetupResult is the image setup made, or found ready.
type SetupResult struct {
	Image            string
	Release          macos.Release
	MacPorts         string
	CommandLineTools string
	// Reused is an image that was already there and passed its checks.
	Reused bool
}

// Setup makes a release's base image with dockhand's provisioner: from
// Cirrus Labs' vanilla macOS image, with the Command Line Tools of the
// release's pinned generation and MacPorts. It keeps a golden copy that a
// lost image is restored from. An image that exists is checked in a
// disposable clone instead, and left as it is. Progress is written to
// progress as it goes.
func (p *Provider) Setup(ctx context.Context, options SetupOptions, progress io.Writer) (SetupResult, error) {
	if options.Check && options.Rebuild {
		return SetupResult{}, errors.New("--check and --rebuild ask for opposite things; choose one")
	}
	release, err := p.release(options.Release)
	if err != nil {
		return SetupResult{}, err
	}
	// Making an image is the costly case, and it's named before it
	// starts: an image that exists, or its golden copy, costs a check.
	if !options.Check && progress != nil {
		m, err := p.vms()
		if err != nil {
			return SetupResult{}, err
		}
		images, err := m.Images(ctx)
		if err != nil {
			return SetupResult{}, fmt.Errorf("listing dockhand's Tart images: %w", err)
		}
		if options.Rebuild || !slices.Contains(images, baseImage(release)) && !slices.Contains(images, goldenImage(release)) {
			fmt.Fprintf(progress, "Making %s for macOS %s (%s), which takes up to %s of disk.\n", baseImage(release), release.Product, release.Name, SetupDisk)
		}
	}
	provisioner := provision.Provisioner{Progress: progress, Config: provision.Config{
		Executable: p.Tart.Executable, Home: p.Tart.Home, MacPortsVersion: options.MacPortsVersion,
		Platform: model.Platform{OS: "darwin", Version: strconv.Itoa(release.Darwin), Architecture: "arm64"},
	}}
	result, err := provisioner.Run(ctx, provision.Options{Check: options.Check, Rebuild: options.Rebuild})
	if err != nil {
		return SetupResult{}, err
	}
	return SetupResult{Image: result.Image, Release: release, MacPorts: result.MacPortsVersion,
		CommandLineTools: result.CommandLineTools, Reused: result.Reused}, nil
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
	out, err := exec.CommandContext(ctx, filepath.Join(filepath.Dir(tclsh), "port"), "version").Output()
	if err != nil {
		return ""
	}
	version, _ := installation.ParseVersion(out)
	return version
}
