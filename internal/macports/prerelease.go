package macports

import (
	"regexp"
	"strings"
)

// Prerelease marks versions that read as something upstream cut before
// it cut a release: alphas, betas, release candidates, snapshots,
// nightlies, and the per-PR CI tags a forge produces by the thousand.
//
// It lives beside VerCmp because two layers ask the same question and
// they must not answer it differently. The planners ask it about a
// version they are being offered — a deliberately conservative livecheck
// must not be charged with rot when only prereleases are newer — and the
// mint asks it about the target a change was minted against, to hold that
// change back from an unattended publication. Two regexps would be two
// heuristics inside a month, and the second would be the one nobody
// remembered to fix.
//
// A version's STYLE is upstream's fact and not MacPorts', so this is the
// one thing in the root that is not base's. It is here because the root
// imports nothing of dockhand's, which makes it the only leaf the mint
// and the planners can both already reach without either importing the
// other — the same property that lets record and publish both import it.
// It arrived from the deleted verdict package, which was the previous
// answer to the same question of where a lone shared judgment goes.
//
// Name-based and imperfect, by design. The forge API's own prerelease
// flag is the authoritative refinement and it is gated on routing tag
// resolution through the authenticated gh seam, which the tag path is
// API-free to avoid. Until then this is a judgment made from a string,
// and it breaks the design's rule 7 in the open: "not a prerelease" and
// "I could not find out authoritatively" are one bool here. So nothing
// that costs anything may gate on one side of it alone — the mint's hold
// is a person's to lift, and a crossing asks about BOTH sides of a move
// so that a misread style cancels rather than decides.
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
