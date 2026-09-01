// Package upstream answers "what is the newest version?" by asking two
// independent witnesses and judging their agreement. The livecheck
// resolver executes the port's own livecheck — the maintainer's
// declared update policy. The forge resolver lists the upstream
// repository's tags — raw reality. Neither is authoritative alone:
// their agreement is a high-confidence answer, and their disagreement
// is a finding — most valuably, livecheck rot silently hiding real
// releases. Corroboration, one level up from spans and deltas.
//
// Version ordering is always MacPorts' own (macports.VerCmp, the pure
// port of base's vercomp.c); this package never invents an ordering.
//
// Planned, deliberately not yet built: when the first registry
// resolver arrives (CRAN, rubygems, PyPI, CPAN — carriers with no git
// forge), version listing becomes a Source interface —
// Versions(ctx) ([]string, error) — with the entire git clan as ONE
// implementation behind Tags and each registry as its own. That is the
// honest granularity: eight forges share one behavior; registries
// genuinely differ. Do not introduce per-forge tag implementations
// before then — the git clan's uniformity (one unauthenticated
// ls-remote, no quotas, self-hosted instances included) is a design
// strength, not an accident.
package upstream

import (
	"regexp"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
)

// Verdict classifies what the two resolvers said.
type Verdict int

const (
	// NoSignal means neither resolver produced a version.
	NoSignal Verdict = iota
	// Agreement means both resolvers name the same newest version.
	Agreement
	// LivecheckOnly means the carrier has no git forge to ask; the
	// livecheck answer stands alone.
	LivecheckOnly
	// ForgeOnly means livecheck is disabled or of an unsupported type;
	// the forge answer stands alone.
	ForgeOnly
	// LivecheckRot means livecheck ran and matched nothing while the
	// forge has versions: the regex is broken, and releases are being
	// silently ignored.
	LivecheckRot
	// LivecheckBehind means the forge has a newer stable version than
	// livecheck reports: the regex matches, but misses newer releases.
	LivecheckBehind
	// LivecheckAhead means livecheck reports a version newer than any
	// forge tag: it may be watching the wrong project, or the upstream
	// moved.
	LivecheckAhead
	// PrereleaseNewest means livecheck and the forge agree on the raw
	// newest tag, but that tag is prerelease-style: the upstream's
	// current releases are all betas. Declining is right; the message
	// must not read as a livecheck fault.
	PrereleaseNewest
	// PrereleaseSuperseded means livecheck matched a prerelease whose
	// release the forge has: semver puts 1.17.0-rc.3 strictly before
	// 1.17.0, so the release stands and resolution proceeds with it.
	// MacPorts VerCmp orders the rc ABOVE its release (a trailing
	// segment reads as more version), which is right for the suffix
	// styles committed Portfiles use — so the precedence lives here in
	// the judgment, not in the comparator (field-measured on gopass,
	// declined "livecheck ahead" with the right answer in hand).
	PrereleaseSuperseded
)

func (v Verdict) String() string {
	switch v {
	case NoSignal:
		return "no signal"
	case Agreement:
		return "agreement"
	case LivecheckOnly:
		return "livecheck only"
	case ForgeOnly:
		return "forge only"
	case LivecheckRot:
		return "livecheck rot: regex matches nothing"
	case LivecheckBehind:
		return "livecheck behind: newer stable-looking tags exist"
	case LivecheckAhead:
		return "livecheck ahead of the forge"
	case PrereleaseNewest:
		return "the newest releases are prerelease-style tags"
	case PrereleaseSuperseded:
		return "livecheck matched a prerelease; the release stands"
	}
	return "unknown verdict"
}

// Observation is the raw testimony Judge rules on.
type Observation struct {
	// LivecheckDisabled: livecheck.type is none, or a type dockhand
	// does not execute — an absent witness, never charged with rot.
	LivecheckDisabled bool
	// Livecheck is the executed livecheck's answer; empty means the
	// regex matched nothing.
	Livecheck string
	// ForgeVersions are the forge-derived versions; nil means the
	// carrier has no git forge to ask.
	ForgeVersions []string
	// Authoritative says ForgeVersions came from the forge's releases
	// API — upstream's own stability flag, already filtered — so the
	// name heuristic has nothing left to judge.
	Authoritative bool
}

// Report is the judged answer.
type Report struct {
	Verdict Verdict
	// Latest is the version --to latest may trust; empty when the
	// verdict is a disagreement or no signal — refusal, not a guess.
	Latest string
	// The individual testimonies, for rendering and findings.
	Livecheck         string
	ForgeNewest       string
	ForgeNewestStable string
	Detail            string
}

// prerelease marks versions the stable-newest comparison excludes: the
// heuristic that keeps a deliberately conservative livecheck from being
// charged with rot when only prereleases are newer. Name-based and
// imperfect; the forge API's authoritative flag is a future refinement.
// pr<digits> is the CI-build spelling flyctl field-tested: per-PR tags
// (v2026.9.1-pr5150.5) that never become releases, which the stable
// heuristic read as stable and then outranked the real newest with. A
// version literally tagged -pr1 is a PR build by any reasonable
// reading. The authoritative refinement remains the forge API's own
// prerelease flag, gated on routing resolution through the
// authenticated gh seam (the tag path is API-free by design).
var prerelease = regexp.MustCompile(`(?i)(^|[^a-z])(alpha|beta|rc|pre|preview|dev|snapshot|nightly|pr[0-9]+)([^a-z]|$|[0-9])`)

// Stable reports whether a version looks like a stable release.
func Stable(version string) bool {
	return !prerelease.MatchString(version)
}

// Judge rules on an observation. Ordering is macports.VerCmp — a pure
// comparison, so judging cannot fail.
func Judge(obs Observation) Report {
	r := Report{Livecheck: obs.Livecheck}
	r.ForgeNewest = newest(obs.ForgeVersions)
	if obs.Authoritative {
		r.ForgeNewestStable = r.ForgeNewest
	} else {
		r.ForgeNewestStable = newest(stableOf(obs.ForgeVersions))
	}

	switch {
	case obs.ForgeVersions == nil && obs.Livecheck == "":
		r.Verdict = NoSignal
		return r
	case obs.ForgeVersions == nil:
		r.Verdict, r.Latest = LivecheckOnly, obs.Livecheck
		return r
	case obs.LivecheckDisabled:
		r.Verdict = ForgeOnly
		if r.Latest = r.ForgeNewestStable; r.Latest == "" {
			r.Latest = r.ForgeNewest
			r.Detail = "only prerelease tags exist"
		}
		return r
	case obs.Livecheck == "":
		r.Verdict = LivecheckRot
		r.Detail = "forge has " + r.ForgeNewest
		return r
	}

	against := r.ForgeNewestStable
	if against == "" {
		// Only prerelease tags: livecheck matching none of them is
		// policy, not rot.
		r.Verdict, r.Latest = Agreement, obs.Livecheck
		r.Detail = "forge has only prerelease tags"
		return r
	}
	c := macports.VerCmp(against, obs.Livecheck)
	switch {
	case c > 0:
		r.Verdict = LivecheckBehind
		r.Detail = "livecheck " + obs.Livecheck + ", newest stable-looking tag " + against
	case c < 0:
		// Semver precedence, judged here rather than in VerCmp: a
		// prerelease orders strictly before its release, so when
		// livecheck matched one and the forge has its release (or
		// newer), the release is the answer, not a disagreement.
		if base, ok := releaseBase(obs.Livecheck); ok && macports.VerCmp(base, against) <= 0 {
			r.Verdict, r.Latest = PrereleaseSuperseded, against
			r.Detail = "livecheck matched prerelease " + obs.Livecheck + "; the release " + against + " stands"
			return r
		}
		if macports.VerCmp(obs.Livecheck, r.ForgeNewest) == 0 {
			// livecheck tracks the forge's raw newest fine; it is ahead
			// only of the stable subset. Charging livecheck here sent a
			// field run chasing a livecheck bug that did not exist —
			// the message printed the newest it never compared against.
			r.Verdict = PrereleaseNewest
			r.Detail = "newest tag " + r.ForgeNewest + " is prerelease-style; newest stable is " + against
			return r
		}
		r.Verdict = LivecheckAhead
		r.Detail = "livecheck " + obs.Livecheck + ", newer than any forge tag (newest " + r.ForgeNewest + ", stable " + against + ")"
	default:
		// vercmp-equal; the maintainer's spelling wins.
		r.Verdict, r.Latest = Agreement, obs.Livecheck
	}
	return r
}

// releaseBase is the release a prerelease-styled version belongs to:
// the part before its prerelease token, separators trimmed —
// 1.17.0-rc.3 belongs to 1.17.0. Not-ok for a stable version, or one
// that is nothing but prerelease token (no base to speak of).
func releaseBase(version string) (string, bool) {
	loc := prerelease.FindStringSubmatchIndex(version)
	if loc == nil {
		return "", false
	}
	base := strings.TrimRight(version[:loc[4]], "-._")
	return base, base != ""
}

func stableOf(versions []string) []string {
	var out []string
	for _, v := range versions {
		if Stable(v) {
			out = append(out, v)
		}
	}
	return out
}

func newest(versions []string) string {
	best := ""
	for _, v := range versions {
		if best == "" || macports.VerCmp(v, best) > 0 {
			best = v
		}
	}
	return best
}
