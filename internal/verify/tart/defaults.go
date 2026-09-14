package tart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/record"
)

type MacOSRelease struct {
	Darwin  int
	Product string
	Name    string
	Slug    string
}

func ReleaseForPlatform(platform record.Platform) (MacOSRelease, error) {
	if platform.OS != "darwin" || platform.Architecture != "arm64" {
		return MacOSRelease{}, fmt.Errorf("tart: unsupported setup platform %s %s", platform.OS, platform.Architecture)
	}
	darwin, err := strconv.Atoi(platform.Version)
	if err != nil {
		return MacOSRelease{}, fmt.Errorf("tart: invalid Darwin version %q", platform.Version)
	}
	releases := map[int]MacOSRelease{
		21: {Darwin: 21, Product: "12", Name: "Monterey", Slug: "monterey"},
		22: {Darwin: 22, Product: "13", Name: "Ventura", Slug: "ventura"},
		23: {Darwin: 23, Product: "14", Name: "Sonoma", Slug: "sonoma"},
		24: {Darwin: 24, Product: "15", Name: "Sequoia", Slug: "sequoia"},
		25: {Darwin: 25, Product: "26", Name: "Tahoe", Slug: "tahoe"},
	}
	release, ok := releases[darwin]
	if !ok {
		return MacOSRelease{}, fmt.Errorf("tart: no provisionable macOS image for Darwin %d", darwin)
	}
	return release, nil
}

func DefaultImageName(platform record.Platform) (string, error) {
	release, err := ReleaseForPlatform(platform)
	if err != nil {
		return "", err
	}
	return "dockhand-base-" + release.Slug, nil
}

func DefaultSource(platform record.Platform) (string, error) {
	release, err := ReleaseForPlatform(platform)
	if err != nil {
		return "", err
	}
	return "ghcr.io/cirruslabs/macos-" + strings.ToLower(release.Slug) + "-vanilla:latest", nil
}
