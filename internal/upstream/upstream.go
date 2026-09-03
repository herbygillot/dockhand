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
	// TagWithoutRelease means livecheck named a version the forge has
	// only as a tag: the releases feed is authoritative about which
	// tags are RELEASES, but an upstream that tags without cutting one
	// (the gopass satellite repos, field-measured) has not disagreed —
	// it has abstained. Livecheck is the maintainer's declared policy
	// and the tag is upstream's own ref: two witnesses agreeing, so
	// resolution proceeds.
	TagWithoutRelease
	// PrereleaseLateral means the port itself rides prereleases —
	// upstream has never cut anything else — and the newest is a
	// higher prerelease: alpha to alpha gives up no stability, so
	// resolution proceeds, named so the maintainer sees the move for
	// what it is (field-measured on amber-lang, whose only possible
	// update path a stricter rule had closed).
	PrereleaseLateral
	// PrereleaseSuperseded means livecheck matched a prerelease whose
	// release the forge has: semver puts 1.17.0-rc.3 strictly before
	// 1.17.0, so the release stands and resolution proceeds with it.
	// MacPorts VerCmp orders the rc ABOVE its release (a trailing
	// segment reads as more version), which is right for the suffix
	// styles committed Portfiles use — so the precedence lives here in
	// the judgment, not in the comparator (field-measured on gopass,
	// declined "livecheck ahead" with the right answer in hand).
	PrereleaseSuperseded
	// LivecheckUncorroborated means livecheck named a stable-looking
	// version and the forge, which was asked and answered, holds
	// nothing that speaks to it: every tag it has is prerelease-style
	// and none of them is that version. One witness spoke to the
	// value.
	//
	// It is not Agreement, which claims two witnesses named the same
	// version — the forge named no such thing. It is not LivecheckOnly
	// either, which is the honest single witness of a carrier with no
	// forge to ask; here there IS a forge and it was asked. Naming the
	// difference is the point: this used to wear the Agreement label
	// and resolve, which published a version on one witness's word
	// while telling the reader two had agreed.
	LivecheckUncorroborated
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
	case TagWithoutRelease:
		return "tagged upstream, but no release cut"
	case PrereleaseLateral:
		return "prerelease to prerelease: nothing stable is given up"
	case PrereleaseSuperseded:
		return "livecheck matched a prerelease; the release stands"
	case LivecheckUncorroborated:
		return "livecheck stands alone: no forge tag corroborates it"
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
	// ForgeVersions are the forge-derived versions; empty means no
	// forge testimony at all. Nil is the carrier with no git forge to
	// ask, and an empty list is a forge that was asked and named
	// nothing — the same absence, judged the same way, because a rule
	// that told them apart would have to say what the second one
	// witnessed, and it witnessed nothing.
	ForgeVersions []string
	// Authoritative says ForgeVersions came from the forge's releases
	// API — upstream's own stability flag, already filtered. The name
	// heuristic still judges what remains: flags are a filter, not a
	// verdict.
	Authoritative bool
	// Current is the version the port rides today, as livecheck
	// checked against it — the witness that tells a stability
	// REGRESSION (stable port offered a beta) from a lateral move (a
	// port already on prereleases following them).
	Current string
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
//
// The four gates below decide who spoke at all; the three arms under
// them decide what was said, and each of those is a flat ordered rule
// list in its own function rather than conditions nested in a switch.
// The shape is deliberate and was bought at a price: two arms published
// a falsehood — a beta resolved as a version, and a single witness
// labelled agreement — and both hid in the nesting rather than in any
// one predicate.
func Judge(obs Observation) Report {
	r := Report{Livecheck: obs.Livecheck}
	r.ForgeNewest = newest(obs.ForgeVersions)
	// The stability witnesses COMPOSE, authoritative or not: the
	// releases feed drops what upstream disclaims, and the name
	// heuristic drops what the version itself disclaims. mergestat
	// field-proved that trusting the flag alone plans a bump to a
	// -beta — upstream publishes beta-named releases with
	// prerelease=false, and an upstream sloppy with its flags is
	// exactly what a second witness is for. A version named -beta is
	// a beta by any reasonable reading, whatever the flag says.
	r.ForgeNewestStable = newest(stableOf(obs.ForgeVersions))

	switch {
	case len(obs.ForgeVersions) == 0 && obs.Livecheck == "":
		r.Verdict = NoSignal
		return r
	case len(obs.ForgeVersions) == 0:
		r.Verdict, r.Latest = LivecheckOnly, obs.Livecheck
		return r
	case obs.LivecheckDisabled:
		return judgeForgeAlone(obs, r)
	case obs.Livecheck == "":
		r.Verdict = LivecheckRot
		r.Detail = "forge has " + r.ForgeNewest
		return r
	}

	if r.ForgeNewestStable == "" {
		return judgeOnlyPrereleaseTags(obs, r)
	}
	against := r.ForgeNewestStable
	switch c := macports.VerCmp(against, obs.Livecheck); {
	case c > 0:
		r.Verdict = LivecheckBehind
		r.Detail = "livecheck " + obs.Livecheck + ", newest stable-looking tag " + against
	case c < 0:
		return judgeLivecheckAboveStable(obs, r, against)
	default:
		// vercmp-equal; the maintainer's spelling wins.
		r.Verdict, r.Latest = Agreement, obs.Livecheck
	}
	return r
}

// judgeForgeAlone rules when livecheck is an absent witness by the
// maintainer's own declaration — never charged with rot — so the forge
// is the only witness this port offers, and it answered completely.
func judgeForgeAlone(obs Observation, r Report) Report {
	if r.ForgeNewestStable == "" {
		// The same two rules the livecheck-enabled arm applies when the
		// forge has cut nothing stable, in the same order, because the
		// question they answer is about the PORT and the FORGE and
		// livecheck is not a term in it. Which witness is absent decides
		// how many spoke, not what a stability regression is.
		//
		// 1. A port already riding prereleases follows them upward
		// without giving up stability. Refusing here would re-close the
		// one update path PrereleaseLateral exists to keep open — the
		// verdict was entered on a field case for exactly this shape,
		// and losing it to the disabled path would undo that ruling by
		// a different route.
		if obs.Current != "" && !Stable(obs.Current) &&
			macports.VerCmp(r.ForgeNewest, obs.Current) > 0 {
			r.Verdict, r.Latest = PrereleaseLateral, r.ForgeNewest
			r.Detail = "livecheck is disabled; the port rides prereleases (" + obs.Current +
				") and upstream has cut nothing stable"
			return r
		}
		// 2. No lateral escape, so resolving would write a -beta into a
		// Portfile as the version — the one thing every other arm of
		// this function refuses, and the falsehood this branch used to
		// publish. The refusal is dockhand's own opinion of sound
		// testimony, so it takes the PrereleaseNewest shape and its
		// band, and the remedy is --to.
		r.Verdict = PrereleaseNewest
		r.Detail = "livecheck is disabled and the forge's newest tag " + r.ForgeNewest +
			" is prerelease-style, with no stable version behind it"
		return r
	}
	r.Verdict, r.Latest = ForgeOnly, r.ForgeNewestStable
	return r
}

// judgeOnlyPrereleaseTags rules when every version the forge lists is
// prerelease-style, so there is no stable tag to compare against.
//
// Ordered rules, most specific first, one guard each: this arm carried
// its three outcomes as nested conditions, and a falsehood hid in the
// nesting for as long as it existed.
func judgeOnlyPrereleaseTags(obs Observation, r Report) Report {
	// 1. A port already riding prereleases — upstream has never cut
	// anything else — gives up no stability by following them upward,
	// and refusing closes the port's only update path. First because
	// it is the narrowest: a known prerelease current version, and a
	// strictly upward move off it.
	if !Stable(obs.Livecheck) && obs.Current != "" && !Stable(obs.Current) &&
		macports.VerCmp(obs.Livecheck, obs.Current) > 0 {
		r.Verdict, r.Latest = PrereleaseLateral, obs.Livecheck
		r.Detail = "the port rides prereleases (" + obs.Current + ") and upstream has cut nothing stable"
		return r
	}
	// 2. Livecheck's own answer is prerelease-style too and rule 1
	// found no lateral escape: resolving would put a -beta in a
	// Portfile as the version. Decline, and never charge livecheck.
	if !Stable(obs.Livecheck) {
		r.Verdict = PrereleaseNewest
		r.Detail = "newest tag " + r.ForgeNewest + " is prerelease-style, and no stable version exists"
		return r
	}
	// 3. A stable-looking livecheck answer that no forge tag is equal
	// to. Livecheck matching none of them is policy rather than rot,
	// and that much was always right — but policy is ONE witness, and
	// calling it agreement claimed a second one that never spoke to
	// this version. The forge ran, answered, and corroborated nothing.
	if !corroborated(obs.ForgeVersions, obs.Livecheck) {
		r.Verdict = LivecheckUncorroborated
		r.Detail = "livecheck " + obs.Livecheck + " stands alone: the forge's tags are all prerelease-style (newest " +
			r.ForgeNewest + ") and none of them is that version"
		return r
	}
	// 4. A tag IS that version. Both witnesses named it, so they
	// agree, whatever the tag's own spelling made the stable subset
	// think of it.
	r.Verdict, r.Latest = Agreement, obs.Livecheck
	r.Detail = "forge has only prerelease tags, and one of them is livecheck's answer"
	return r
}

// judgeLivecheckAboveStable rules when livecheck's answer outranks the
// newest stable-looking tag. Ordered rules, most specific first, one
// guard each, for the same reason as the arm above: three outcomes
// nested inside one another are three outcomes nobody can audit.
func judgeLivecheckAboveStable(obs Observation, r Report, against string) Report {
	// 1. Semver precedence, judged here rather than in VerCmp: a
	// prerelease orders strictly before its release, so when livecheck
	// matched one and the forge has its release (or newer), the
	// release is the answer and not a disagreement. First because it
	// is the only rule here that resolves.
	if base, ok := releaseBase(obs.Livecheck); ok && macports.VerCmp(base, against) <= 0 {
		r.Verdict, r.Latest = PrereleaseSuperseded, against
		r.Detail = "livecheck matched prerelease " + obs.Livecheck + "; the release " + against + " stands"
		return r
	}
	// 2. Livecheck's answer IS the forge's raw newest: it tracks the
	// forge fine and is ahead only of the stable subset. Charging
	// livecheck here sent a field run chasing a livecheck bug that did
	// not exist — the message printed the newest it never compared
	// against.
	if macports.VerCmp(obs.Livecheck, r.ForgeNewest) == 0 {
		r.Verdict = PrereleaseNewest
		r.Detail = "newest tag " + r.ForgeNewest + " is prerelease-style; newest stable is " + against
		return r
	}
	// 3. Nothing the forge holds accounts for livecheck's answer: it
	// may be watching the wrong project, or upstream moved. The detail
	// names what was actually compared, because saying "tag" over an
	// authoritative releases feed misled a field run — the tag may
	// well exist, and check.go goes and asks for it next.
	r.Verdict = LivecheckAhead
	if obs.Authoritative {
		r.Detail = "livecheck " + obs.Livecheck + ", newer than any forge release (newest " + r.ForgeNewest + ")"
	} else {
		r.Detail = "livecheck " + obs.Livecheck + ", newer than any forge tag (newest " + r.ForgeNewest + ", stable " + against + ")"
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

// corroborate re-judges a LivecheckAhead-of-releases report against
// the tag list, the second witness the releases feed cannot speak
// for. A tag vercmp-equal to livecheck's answer resolves the report;
// its absence hardens the decline's message.
func corroborate(r Report, tags []string) Report {
	if corroborated(tags, r.Livecheck) {
		r.Verdict, r.Latest = TagWithoutRelease, r.Livecheck
		r.Detail = "livecheck " + r.Livecheck + " matches a forge tag; upstream cut no release for it"
		return r
	}
	r.Detail += "; no forge tag matches either"
	return r
}

// corroborated reports whether the forge holds the exact version
// livecheck named — the membership test that turns one witness into
// two, in the single spelling both askers share. It was a loop inside
// corroborate while only one arm asked; the second arm that needed it
// was written without it, and shipped an Agreement nobody had
// corroborated.
func corroborated(versions []string, v string) bool {
	for _, t := range versions {
		if macports.VerCmp(t, v) == 0 {
			return true
		}
	}
	return false
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
