package tart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/record"
)

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
