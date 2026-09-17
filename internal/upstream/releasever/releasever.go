// Package releasever classifies upstream version spellings as stable or
// prerelease. Automatic selection admits stable versions only; an explicit
// version is always honored, and callers use the classification to say when a
// change takes a port out of stable rather than to refuse it.
package releasever

import (
	"regexp"
	"strings"
)

// Stability is the classification of one version spelling.
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
	stable = regexp.MustCompile(`^[0-9]+(?:[.-][0-9]+)*$`)
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
