package version

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInput means a requested spelling cannot be a version or tag.
var ErrInput = errors.New("version: invalid spelling")

// Validate accepts any non-empty spelling without whitespace, control
// characters, or a leading dash, which would read as an option.
func Validate(value string) error {
	if value == "" || !utf8.ValidString(value) || strings.HasPrefix(value, "-") || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return ErrInput
	}
	return nil
}

// TagPattern maps between a Portfile version and the upstream tag that
// carries it, such as v1.2.3 for 1.2.3.
type TagPattern struct {
	Prefix string
	Suffix string
}

func (p TagPattern) Tag(version string) string { return p.Prefix + version + p.Suffix }

// Version strips the pattern from a tag; ok is false when the tag does not fit.
func (p TagPattern) Version(tag string) (string, bool) {
	version, prefix := strings.CutPrefix(tag, p.Prefix)
	version, suffix := strings.CutSuffix(version, p.Suffix)
	return version, prefix && suffix && version != ""
}

// Explicit reports whether a requested spelling already carries the pattern,
// so it names a tag rather than a version.
func (p TagPattern) Explicit(value string) bool {
	return p.Prefix != "" && strings.HasPrefix(value, p.Prefix) || p.Suffix != "" && strings.HasSuffix(value, p.Suffix)
}

// Stability is the classification of one version spelling. Automatic
// selection admits stable versions, and prereleases only for a port already
// on one; an explicit version is always honored, and callers use the
// classification to say when a change takes a port out of stable.
type Stability string

const (
	// Stable is a numeric version: dotted or dashed integers, including calendar versions.
	Stable Stability = "stable"
	// Prerelease carries a recognized pre-release marker such as alpha, beta, or rc.
	Prerelease Stability = "prerelease"
	// Unknown is any other spelling; it is neither admitted automatically nor called a prerelease.
	Unknown Stability = "unknown"
)

var (
	// A leading v, as perl's dotted-decimal module versions spell it, is
	// part of a stable spelling.
	stable = regexp.MustCompile(`^v?[0-9]+(?:[._-][0-9]+)*$`)
	// pep440 covers 1.0a1 and 1.0b2, where a single letter between digits is a
	// pre-release segment rather than a patch letter such as 1.0.2u.
	pep440  = regexp.MustCompile(`[0-9](?:a|b|c|rc)[0-9]`)
	markers = map[string]bool{
		"alpha": true, "beta": true, "rc": true, "pre": true, "prerelease": true, "preview": true,
		"dev": true, "snapshot": true, "nightly": true, "canary": true, "next": true, "unstable": true,
		"milestone": true, "cr": true, "test": true,
	}
)

// Classify reports the stability of a version spelling.
func Classify(version string) Stability {
	if stable.MatchString(version) {
		return Stable
	}
	lower := strings.ToLower(version)
	for _, run := range strings.FieldsFunc(lower, func(r rune) bool { return r < 'a' || r > 'z' }) {
		if markers[run] {
			return Prerelease
		}
	}
	if pep440.MatchString(lower) {
		return Prerelease
	}
	return Unknown
}

// LeavesStable reports whether moving from one version to another takes a
// port from a stable version to a prerelease.
func LeavesStable(from, to string) bool {
	return Classify(from) == Stable && Classify(to) == Prerelease
}

// leading is a version's first numeric component, without a leading v.
var leading = regexp.MustCompile(`^v?([0-9]+)`)

// CrossesMajor reports whether moving from one version to another changes
// its first numeric component, semgrep's 0.14.0 to 1.179.0: the change a
// project's own numbering says may break what builds on it. False where
// either has none, and where one is above 999, a year or a date, as
// 2025.1 or 20250914, whose first component moves every year.
func CrossesMajor(from, to string) bool {
	a, b := leading.FindStringSubmatch(from), leading.FindStringSubmatch(to)
	if a == nil || b == nil {
		return false
	}
	x, y := strings.TrimLeft(a[1], "0"), strings.TrimLeft(b[1], "0")
	return x != y && len(x) <= 3 && len(y) <= 3
}
