package tart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/record"
)

// DefaultDarwin is the newest macOS a Tart build uses without being asked, the
// current release. Local verification should prove what a MacPorts pull
// request is built on, and a Tart build runs on its image's release and no
// other, so a host that upgrades is asked rather than assumed: --os names the
// release for setup, --image selects its image.
const DefaultDarwin = macos.CurrentDarwin

// DefaultRelease is the release Tart builds on unasked.
func DefaultRelease() (macos.Release, error) { return macos.ReleaseForDarwin(DefaultDarwin) }

// NewerThanDefault reports whether a release is past it.
func NewerThanDefault(release macos.Release) bool { return release.Darwin > DefaultDarwin }

func ReleaseForPlatform(platform record.Platform) (macos.Release, error) {
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

// DefaultImageName returns the conventional command-line-tools image name.
func DefaultImageName(platform record.Platform) (string, error) {
	release, err := ReleaseForPlatform(platform)
	if err != nil {
		return "", err
	}
	return "dockhand-base-" + release.Slug, nil
}

// DefaultXcodeImageName returns the conventional full-Xcode image name.
func DefaultXcodeImageName(platform record.Platform) (string, error) {
	release, err := ReleaseForPlatform(platform)
	if err != nil {
		return "", err
	}
	return "dockhand-xcode-" + release.Slug, nil
}

// DefaultSource returns the vanilla OCI image used to provision a platform.
func DefaultSource(platform record.Platform) (string, error) {
	release, err := ReleaseForPlatform(platform)
	if err != nil {
		return "", err
	}
	return "ghcr.io/cirruslabs/macos-" + strings.ToLower(release.Slug) + "-vanilla:latest", nil
}

// PreparedReleases are the releases a local image serves under setup's
// names, a command-line-tools or a full-Xcode image, oldest first.
func PreparedReleases(images []Image) []macos.Release {
	var prepared []macos.Release
	for _, release := range macos.Known() {
		for _, image := range images {
			if image.Source == "local" && (image.Name == "dockhand-base-"+release.Slug || image.Name == "dockhand-xcode-"+release.Slug) {
				prepared = append(prepared, release)
				break
			}
		}
	}
	return prepared
}
