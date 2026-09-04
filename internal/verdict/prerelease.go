package verdict

import (
	"regexp"
	"strings"
)

// Prerelease marks versions that read as something upstream cut before
// it cut a release: alphas, betas, release candidates, snapshots,
// nightlies, and the per-PR CI tags a forge produces by the thousand.
//
// It lives here because two layers now ask the same question and they
// must not answer it differently. The planners ask it about a version
// they are being offered — a deliberately conservative livecheck must
// not be charged with rot when only prereleases are newer — and the mint
// asks it about the target a change was minted against, to hold that
// change back from an unattended publication. Two regexps would be two
// heuristics inside a month, and the second would be the one nobody
// remembered to fix.
//
// Name-based and imperfect, by design. The forge API's own prerelease
// flag is the authoritative refinement and it is gated on routing tag
// resolution through the authenticated gh seam, which the tag path is
// API-free to avoid. Until then this is a judgment made from a string,
// which is exactly the kind of thing this package is for.
//
// pr<digits> is the CI-build spelling flyctl field-tested: per-PR tags
// (v2026.9.1-pr5150.5) that never become releases, which the stable
// heuristic read as stable and then outranked the real newest with. A
// version literally tagged -pr1 is a PR build by any reasonable reading.
func Prerelease(version string) bool { return prerelease.MatchString(version) }

// PrereleaseBase is the release a prerelease-styled version belongs to:
// the part before its prerelease token, separators trimmed —
// 1.17.0-rc.3 belongs to 1.17.0. Not-ok for a stable version, or one
// that is nothing but prerelease token (no base to speak of).
func PrereleaseBase(version string) (string, bool) {
	loc := prerelease.FindStringSubmatchIndex(version)
	if loc == nil {
		return "", false
	}
	base := strings.TrimRight(version[:loc[4]], "-._")
	return base, base != ""
}

// prerelease is the heuristic itself, spelled once.
var prerelease = regexp.MustCompile(`(?i)(^|[^a-z])(alpha|beta|rc|pre|preview|dev|snapshot|nightly|pr[0-9]+)([^a-z]|$|[0-9])`)
