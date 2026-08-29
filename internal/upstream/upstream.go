// Package upstream answers "what is the newest version?" by asking two
// independent witnesses and judging their agreement. The livecheck
// resolver executes the port's own livecheck — the maintainer's
// declared update policy. The forge resolver lists the upstream
// repository's tags — raw reality. Neither is authoritative alone:
// their agreement is a high-confidence answer, and their disagreement
// is a finding — most valuably, livecheck rot silently hiding real
// releases. Corroboration, one level up from spans and deltas.
//
// Version ordering is always MacPorts' own (vercmp through a fetch
// session); this package never invents an ordering.
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
		return "livecheck behind: newer stable releases exist"
	case LivecheckAhead:
		return "livecheck ahead of the forge"
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
	// ForgeVersions are the tag-derived versions; nil means the carrier
	// has no git forge to ask.
	ForgeVersions []string
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
var prerelease = regexp.MustCompile(`(?i)(^|[^a-z])(alpha|beta|rc|pre|preview|dev|snapshot|nightly)([^a-z]|$|[0-9])`)

// Stable reports whether a version looks like a stable release.
func Stable(version string) bool {
	return !prerelease.MatchString(version)
}

// Compare orders two versions: negative, zero, positive. Implemented
// by portfetch's Vercmp — MacPorts' own ordering.
type Compare func(a, b string) (int, error)

// Judge rules on an observation. cmp must be MacPorts ordering.
func Judge(obs Observation, cmp Compare) (Report, error) {
	r := Report{Livecheck: obs.Livecheck}
	var err error
	r.ForgeNewest, err = newest(obs.ForgeVersions, cmp)
	if err != nil {
		return Report{}, err
	}
	r.ForgeNewestStable, err = newest(stableOf(obs.ForgeVersions), cmp)
	if err != nil {
		return Report{}, err
	}

	switch {
	case obs.ForgeVersions == nil && obs.Livecheck == "":
		r.Verdict = NoSignal
		return r, nil
	case obs.ForgeVersions == nil:
		r.Verdict, r.Latest = LivecheckOnly, obs.Livecheck
		return r, nil
	case obs.LivecheckDisabled:
		r.Verdict = ForgeOnly
		if r.Latest = r.ForgeNewestStable; r.Latest == "" {
			r.Latest = r.ForgeNewest
			r.Detail = "only prerelease tags exist"
		}
		return r, nil
	case obs.Livecheck == "":
		r.Verdict = LivecheckRot
		r.Detail = "forge has " + r.ForgeNewest
		return r, nil
	}

	against := r.ForgeNewestStable
	if against == "" {
		// Only prerelease tags: livecheck matching none of them is
		// policy, not rot.
		r.Verdict, r.Latest = Agreement, obs.Livecheck
		r.Detail = "forge has only prerelease tags"
		return r, nil
	}
	c, err := cmp(against, obs.Livecheck)
	if err != nil {
		return Report{}, err
	}
	switch {
	case c > 0:
		r.Verdict = LivecheckBehind
		r.Detail = "livecheck " + obs.Livecheck + ", forge stable " + against
	case c < 0:
		r.Verdict = LivecheckAhead
		r.Detail = "livecheck " + obs.Livecheck + ", forge newest " + r.ForgeNewest
	default:
		// vercmp-equal; the maintainer's spelling wins.
		r.Verdict, r.Latest = Agreement, obs.Livecheck
	}
	return r, nil
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

func newest(versions []string, cmp Compare) (string, error) {
	best := ""
	for _, v := range versions {
		if best == "" {
			best = v
			continue
		}
		c, err := cmp(v, best)
		if err != nil {
			return "", err
		}
		if c > 0 {
			best = v
		}
	}
	return best, nil
}
