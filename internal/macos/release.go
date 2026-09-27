package macos

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/model"
	"slices"
	"strconv"
	"strings"
)

type Release struct {
	Darwin  int
	Product string
	Name    string
	Slug    string
	// Tools is the Command Line Tools generation, the major version, setup
	// installs for this release (decision 13), from the facts table: what
	// MacPorts' arm64 builder for the release runs. It is zero for a
	// release the table has no generation for.
	Tools int
	// Xcode is the Xcode MacPorts' arm64 builder for the release runs,
	// from the facts table: what its Xcode image installs unless the
	// configuration names another. Empty for a release the table has no
	// builder for.
	Xcode string
}

// CurrentDarwin is the macOS a host that is not a Mac models unless told
// otherwise. It is deliberately not the newest release the table carries:
// MacPorts adds a macOS to its own CI well after Apple ships it. A Mac
// describes, and Tart builds on, its own release.
const CurrentDarwin = 25

// releases is keyed by Darwin major, which is not consecutive: Apple
// skipped 26, and Golden Gate, macOS 27, is Darwin 27. Each release's
// tools generation and Xcode come from the facts table.
var releases = withBuilders(map[int]Release{
	21: {Darwin: 21, Product: "12", Name: "Monterey", Slug: "monterey"},
	22: {Darwin: 22, Product: "13", Name: "Ventura", Slug: "ventura"},
	23: {Darwin: 23, Product: "14", Name: "Sonoma", Slug: "sonoma"},
	24: {Darwin: 24, Product: "15", Name: "Sequoia", Slug: "sequoia"},
	25: {Darwin: 25, Product: "26", Name: "Tahoe", Slug: "tahoe"},
	27: {Darwin: 27, Product: "27", Name: "Golden Gate", Slug: "golden-gate"},
})

// withBuilders gives each release what MacPorts' arm64 builder for it
// runs: its tools generation, and its Xcode.
func withBuilders(releases map[int]Release) map[int]Release {
	for darwin, release := range releases {
		release.Tools, _ = Table().Generation(darwin)
		release.Xcode, _ = Table().BuilderXcode(darwin)
		releases[darwin] = release
	}
	return releases
}

// Known lists the releases this table carries, oldest first. It is the one
// place the set is written; callers that need to name it, or to decide whether
// a platform is one of them, read it rather than repeating the range.
func Known() []Release {
	known := make([]Release, 0, len(releases))
	for _, release := range releases {
		known = append(known, release)
	}
	slices.SortFunc(known, func(a, b Release) int { return a.Darwin - b.Darwin })
	return known
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
	var known []string
	for _, release := range Known() {
		known = append(known, release.Slug+" ("+release.Product+")")
	}
	return Release{}, fmt.Errorf("macos: unknown release %q; use %s", value, strings.Join(known, ", "))
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
func Describe(platform model.Platform) string {
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
