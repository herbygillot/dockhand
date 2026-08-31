// Package platform is the reference table for macOS releases: the
// marketing name, the product version, and the Darwin major, which are
// three names for one thing that almost nothing agrees on.
//
// A user says "sequoia". A tart image is macos-sequoia-xcode. A GitHub
// runner is macos-15. MacPorts keys its platform-conditional logic on
// os.major, which is the Darwin major, 24. And the installer for it is
// MacPorts-2.12.6-15-Sequoia.pkg, which uses two of the three at once.
// Every consumer needs a different rendering of the same fact, so the
// fact lives here once and each composes its own name from it.
//
// What is not here is any of those renderings. A tart image name is
// tart's business, a runner label is GitHub's, and an installer file
// name is MacPorts'. This package answers which release is being
// talked about — plus, in host.go, the same kind of measured fact
// about the machine underfoot — and knows nothing about who is
// asking.
//
// Nor is this info.Platform, which is a different thing wearing a
// similar word: that is an evaluation frame in base's own
// plat_major_arch vocabulary, naming an OS flavour and an architecture
// as well. A Release names a macOS release and nothing else; a caller
// that wants a frame builds one from the Darwin major.
package platform

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Release is one macOS release under all three of its names.
type Release struct {
	// Name is Apple's marketing name, capitalized as Apple writes it.
	Name string
	// Product is the macOS version as Apple and GitHub number it. It is
	// a string because the 10.x releases are not integers, and pretending
	// otherwise would silently drop everything before Big Sur.
	Product string
	// Darwin is the kernel major, which is what MacPorts means by
	// os.major and what a Portfile's platform conditionals compare.
	Darwin int
}

// Releases is every release this table knows, oldest first.
//
// The span is not arbitrary: it is what MacPorts still publishes
// installers for, 10.6 through 26. A release this table cannot name is
// one dockhand cannot provision an environment for, so the table ends
// where MacPorts' own support does.
//
// Two entries are verified against running systems rather than
// recalled: Sequoia reports macOS 15.7.7 on Darwin 24.6.0, and Tahoe
// reports 26.6.2 on Darwin 25.6.0 — which is also where Apple's product
// numbering jumps from 15 to 26 while Darwin simply continues.
var Releases = []Release{
	{Name: "Snow Leopard", Product: "10.6", Darwin: 10},
	{Name: "Lion", Product: "10.7", Darwin: 11},
	{Name: "Mountain Lion", Product: "10.8", Darwin: 12},
	{Name: "Mavericks", Product: "10.9", Darwin: 13},
	{Name: "Yosemite", Product: "10.10", Darwin: 14},
	{Name: "El Capitan", Product: "10.11", Darwin: 15},
	{Name: "Sierra", Product: "10.12", Darwin: 16},
	{Name: "High Sierra", Product: "10.13", Darwin: 17},
	{Name: "Mojave", Product: "10.14", Darwin: 18},
	{Name: "Catalina", Product: "10.15", Darwin: 19},
	{Name: "Big Sur", Product: "11", Darwin: 20},
	{Name: "Monterey", Product: "12", Darwin: 21},
	{Name: "Ventura", Product: "13", Darwin: 22},
	{Name: "Sonoma", Product: "14", Darwin: 23},
	{Name: "Sequoia", Product: "15", Darwin: 24},
	{Name: "Tahoe", Product: "26", Darwin: 25},
}

// ErrUnknownRelease reports a release this table does not name. It is
// deliberately an error rather than a guess: a caller that asked for a
// platform dockhand does not know must be told so, not handed the
// nearest one.
var ErrUnknownRelease = errors.New("platform: unknown macOS release")

// String renders a release the way a person would say it.
func (r Release) String() string {
	return fmt.Sprintf("%s (macOS %s, darwin %d)", r.Name, r.Product, r.Darwin)
}

// CompactName is the marketing name without spaces — "BigSur",
// "HighSierra" — which is how MacPorts writes it in an installer file
// name and how tart writes it in an image name. The space is Apple's;
// nearly every tool drops it.
func (r Release) CompactName() string { return strings.ReplaceAll(r.Name, " ", "") }

// IsZero reports the unset release.
func (r Release) IsZero() bool { return r == Release{} }

// ByName finds a release by marketing name, insensitive to case and to
// the separators every tool writes differently — "high sierra",
// "HighSierra", "high-sierra" and "big_sur" all arrive, as does a
// "macos" prefix ("macos-sequoia", the shape an image name puts it in).
func ByName(name string) (Release, bool) {
	want := strings.TrimPrefix(fold(name), "macos")
	for _, r := range Releases {
		if fold(r.Name) == want {
			return r, true
		}
	}
	return Release{}, false
}

// ByProduct finds a release by its macOS version, with or without a
// "macos" prefix, so a GitHub runner label ("macos-14") resolves as
// readily as a number a person typed. A version with more components
// than the table's — "14.5", "15.7.7", "10.15.4" — resolves to its
// release: a person holding a point release still means the release.
func ByProduct(v string) (Release, bool) {
	want := strings.TrimPrefix(fold(v), "macos")
	for _, r := range Releases {
		if fold(r.Product) == want {
			return r, true
		}
	}
	// No exact entry: reduce to the release-naming components — two for
	// the 10.x era, one after it — and try once more.
	parts := strings.Split(want, ".")
	var major string
	switch {
	case len(parts) >= 2 && parts[0] == "10":
		major = parts[0] + "." + parts[1]
	case len(parts) >= 2:
		major = parts[0]
	default:
		return Release{}, false
	}
	if major == want {
		return Release{}, false
	}
	for _, r := range Releases {
		if fold(r.Product) == major {
			return r, true
		}
	}
	return Release{}, false
}

// ByDarwin finds a release by kernel major — the number MacPorts uses,
// and the one uname reports.
func ByDarwin(major int) (Release, bool) {
	for _, r := range Releases {
		if r.Darwin == major {
			return r, true
		}
	}
	return Release{}, false
}

// Parse resolves a release from how a person or a tool is likely to
// write it: a marketing name in any separator style, a product version
// down to the point release, or a "macos"-prefixed label.
//
// A bare number is read as a product version, never as a Darwin major.
// The two ranges overlap — 25 is Tahoe's kernel and 26 is its product —
// so a number that could be either has to mean one, and it means the
// one users and CI labels write. A caller holding a kernel major says
// so with ByDarwin.
func Parse(s string) (Release, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Release{}, fmt.Errorf("%w: empty", ErrUnknownRelease)
	}
	if r, ok := ByName(s); ok {
		return r, nil
	}
	if r, ok := ByProduct(s); ok {
		return r, nil
	}
	return Release{}, fmt.Errorf("%w: %q", ErrUnknownRelease, s)
}

// FromUname resolves a release from the kernel version uname -r reports
// ("24.6.0"), which is how a running system identifies itself when
// sw_vers is not to hand.
func FromUname(release string) (Release, error) {
	major, _, _ := strings.Cut(strings.TrimSpace(release), ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return Release{}, fmt.Errorf("%w: uname reported %q", ErrUnknownRelease, release)
	}
	r, ok := ByDarwin(n)
	if !ok {
		return Release{}, fmt.Errorf("%w: darwin %d", ErrUnknownRelease, n)
	}
	return r, nil
}

// fold normalizes a name for comparison: case, and the separators in
// "High Sierra" — spaces, hyphens, underscores — which every tool
// writes differently.
func fold(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, sep := range []string{" ", "-", "_"} {
		s = strings.ReplaceAll(s, sep, "")
	}
	return s
}
