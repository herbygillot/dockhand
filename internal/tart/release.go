package tart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
)

func ReleaseForPlatform(platform model.Platform) (macos.Release, error) {
	if platform.OS != "darwin" || platform.Architecture != "arm64" {
		return macos.Release{}, fmt.Errorf("tart: unsupported setup platform %s %s", platform.OS, platform.Architecture)
	}
	darwin, err := strconv.Atoi(platform.Version)
	if err != nil {
		return macos.Release{}, fmt.Errorf("tart: invalid Darwin version %q", platform.Version)
	}
	release, err := macos.ReleaseForDarwin(darwin)
	if err != nil {
		return macos.Release{}, fmt.Errorf("tart: no provisionable macOS image for Darwin %d", darwin)
	}
	return release, nil
}

// Prepared is one of dockhand's images: a release's, with a profile's
// developer tools, the Command Line Tools alone or Xcode beside them. Setup
// makes it from the release's vanilla image and keeps a golden copy beside
// it, which a lost image is restored from; checks clone it. Its names are
// spelled here alone.
type Prepared struct {
	Release macos.Release
	Profile macos.Profile
}

// Name is the image's name: dockhand-base-tahoe, or dockhand-xcode-tahoe.
func (p Prepared) Name() string {
	if p.Profile == macos.ProfileXcode {
		return "dockhand-xcode-" + p.Release.Slug
	}
	return "dockhand-base-" + p.Release.Slug
}

// Golden is its golden copy's name: dockhand-golden-tahoe, or
// dockhand-golden-xcode-tahoe.
func (p Prepared) Golden() string {
	if p.Profile == macos.ProfileXcode {
		return "dockhand-golden-xcode-" + p.Release.Slug
	}
	return "dockhand-golden-" + p.Release.Slug
}

// Source is the vanilla image setup starts it from, Cirrus Labs'.
func (p Prepared) Source() string {
	return "ghcr.io/cirruslabs/macos-" + strings.ToLower(p.Release.Slug) + "-vanilla:latest"
}

// ParsePrepared reads an image's name as one of dockhand's images.
func ParsePrepared(name string) (Prepared, bool) {
	for _, release := range macos.Known() {
		for _, profile := range []macos.Profile{macos.ProfileTools, macos.ProfileXcode} {
			if prepared := (Prepared{Release: release, Profile: profile}); prepared.Name() == name {
				return prepared, true
			}
		}
	}
	return Prepared{}, false
}

// GoldenName is an image's golden copy: a prepared image's own, and
// <name>-golden for an image under another name.
func GoldenName(image string) string {
	if prepared, ok := ParsePrepared(image); ok {
		return prepared.Golden()
	}
	return image + "-golden"
}
