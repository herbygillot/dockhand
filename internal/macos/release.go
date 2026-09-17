package macos

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/record"
	"strconv"
	"strings"
)

type Release struct {
	Darwin  int
	Product string
	Name    string
	Slug    string
}

var releases = map[int]Release{
	21: {Darwin: 21, Product: "12", Name: "Monterey", Slug: "monterey"},
	22: {Darwin: 22, Product: "13", Name: "Ventura", Slug: "ventura"},
	23: {Darwin: 23, Product: "14", Name: "Sonoma", Slug: "sonoma"},
	24: {Darwin: 24, Product: "15", Name: "Sequoia", Slug: "sequoia"},
	25: {Darwin: 25, Product: "26", Name: "Tahoe", Slug: "tahoe"},
}

func ReleaseForDarwin(darwin int) (Release, error) {
	release, ok := releases[darwin]
	if !ok {
		return Release{}, fmt.Errorf("macos: unknown release for Darwin %d", darwin)
	}
	return release, nil
}

// ParseRelease accepts a macOS release name or major product version.
func ParseRelease(value string) (Release, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, release := range releases {
		if value == release.Slug || value == release.Product {
			return release, nil
		}
	}
	return Release{}, fmt.Errorf("macos: unknown release %q; use monterey (12), ventura (13), sonoma (14), sequoia (15), or tahoe (26)", value)
}

// ProductForDarwin returns the macOS release used for metadata modeling. This
// does not extend the set of VM releases supported by provisioning.
func ProductForDarwin(darwin int) (string, error) {
	if darwin >= 8 && darwin < 20 {
		return fmt.Sprintf("10.%d", darwin-4), nil
	}
	if darwin == 20 {
		return "11", nil
	}
	release, err := ReleaseForDarwin(darwin)
	return release.Product, err
}

// Describe words a platform for a person unambiguously, "macOS 26 (Tahoe)
// arm64" for darwin 25, and keeps the raw fields when the release is unknown.
func Describe(platform record.Platform) string {
	parts := []string{platform.OS, platform.Version}
	if platform.OS == "darwin" {
		if major, err := strconv.Atoi(platform.Version); err == nil {
			if release, err := ReleaseForDarwin(major); err == nil {
				parts = []string{"macOS", release.Product, "(" + release.Name + ")"}
			}
		}
	}
	if platform.Architecture != "" {
		parts = append(parts, platform.Architecture)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}
