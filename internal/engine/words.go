package engine

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// targetWords is how one target's result in one environment reads, on the
// terminal and in the pull request alike, so the two never word one result
// differently (Design v3 §7): excluded there by the plan, and why, passed, passed
// with its tests failing or timing out where they didn't count, failed at
// a phase, blocked by a failed changed dependency, not run, or interrupted.
// Reading says how the tests counted, "advisory" when empty
// (Evidence.testsReading). Accepted is why submit --accept acknowledged a
// failure, where it did.
func targetWords(plan model.Plan, result Cell, reading string, accepted AcceptedFailure) string {
	if reading == "" {
		reading = "advisory"
	}
	switch result.Kind {
	case CellExcluded:
		// A profile's own choice not to build it there is no result.
		if result.Exclusion.Span {
			return "—"
		}
		// Why the plan left it out is said, as wasmer's macOS 12 column
		// read only "— excluded", where its Portfile marks it known_fail
		// on arm64 before macOS 14 (the wasmer run, #35077).
		if reason := result.Exclusion.Reason; reason != "" {
			return "— not built: " + reason
		}
		return "— excluded"
	case CellUnmet:
		return "! not built: " + UnmetWords(result.Unmet)
	}
	var words string
	switch result.Outcome {
	case model.OutcomePassed:
		switch result.Tests {
		case model.TestsPassed:
			// Tests that ran and passed are said, as failing ones are:
			// s2n-tls +tests read "✓" of its 284 required tests (the
			// s2n-tls run's finding 3).
			return "✓ tests passed"
		case model.TestsFailed:
			return "✓ build passed; tests failed (" + reading + ")"
		case model.TestsTimedOut:
			return "✓ build passed; tests timed out (" + reading + ")"
		case model.TestsNone:
			// Required tests ask nothing of a port that declares none, so its
			// ✓ isn't a pass of tests (the ov run's finding 3).
			if plan.Tests == model.TestsRequired {
				return "✓ declares no tests"
			}
		}
		return "✓"
	case model.OutcomeFailed:
		words = "✗ failed"
		if result.Phase != "" {
			words = "✗ " + result.Phase.FailureWords()
		}
		if result.Phase == model.PhaseTest && result.Tests == model.TestsTimedOut {
			words += ": tests timed out"
		}
	case model.OutcomeBlocked:
		// Blocked only by accepted failures is said so: py-textual-
		// fastdatatable's stub read as an unexplained failure beside its
		// five accepted subports (field testing, #35662).
		if accepted.Kind == AcceptedBlocked {
			return "✗ blocked only by accepted failures"
		}
		words = "✗ blocked by a failed changed dependency"
	case model.OutcomeNotRun:
		return "! not run"
	default:
		return "! " + string(result.Outcome)
	}
	if accepted.Kind != "" {
		words += ", accepted: " + accepted.Words()
	}
	return words
}

// refWords names a Git source's ref as a Portfile gives it: "git.branch
// v1.2", or its repository's default branch where it names none.
func refWords(ref string) string {
	if ref == "" {
		return "the default branch"
	}
	return "git.branch " + ref
}

// expectedWords names what a Git source's build is expected to fetch: a
// commit, shortened, or the abbreviation it begins with.
func expectedWords(source model.GitSource) string {
	if source.Commit != "" {
		return source.Commit.Short()
	}
	return source.Abbreviation
}

// GitSourceWords say what a Git-fetched target's build is expected to
// fetch, as a check is planned: "git.branch v1.2 names 1a2b3c4 now, which
// its build must fetch", or why that isn't known.
func GitSourceWords(source model.GitSource) string {
	return sourceWords(source, "which its build must fetch", "its build records what it fetches, and stands for no later check")
}

// LeftOutSourceWords say what a Git-fetched target --only left out is
// expected to have fetched: the check doesn't build it, but an earlier
// check's result of it stands only where its build fetched the commit its
// git.branch names as the check is planned (Counts). "git.branch v1.2
// names 1a2b3c4 now, which an earlier check's build of it must have
// fetched to stand", or why that isn't known.
func LeftOutSourceWords(source model.GitSource) string {
	return sourceWords(source, "which an earlier check's build of it must have fetched to stand", "no earlier check's result of it stands")
}

// sourceWords say what a Git source names as a check is planned, and what
// that asks (fetch), or why it isn't known, and what that means (unknown).
func sourceWords(source model.GitSource, fetch, unknown string) string {
	switch {
	case source.Commit != "" && strings.EqualFold(source.Ref, string(source.Commit)):
		return fmt.Sprintf("git.branch is commit %s, %s", source.Commit.Short(), fetch)
	case source.Commit != "":
		return fmt.Sprintf("%s names %s now, %s", refWords(source.Ref), source.Commit.Short(), fetch)
	case source.Abbreviation != "":
		return fmt.Sprintf("git.branch abbreviates a commit, %s, %s", source.Abbreviation, fetch)
	}
	return fmt.Sprintf("which commit %s names isn't known (%s); %s", refWords(source.Ref), source.Unresolved, unknown)
}

// movedWords say why a build that fetched another commit than its plan
// expected failed at fetch.
func movedWords(source model.GitSource, fetched model.ObjectID) string {
	return fmt.Sprintf("the source moved: %s named %s when the check was planned, and the build fetched %s; a new check builds what it names now", refWords(source.Ref), expectedWords(source), fetched.Short())
}

// FetchedWords say what a Git-fetched target's build fetched, for its
// evidence: the commit it was expected to, another, or, where its provider
// couldn't say, that it isn't known.
func FetchedWords(source model.GitSource, fetched model.ObjectID) string {
	switch {
	case fetched == "" && source.Expected() == "":
		return fmt.Sprintf("which commit of %s it fetched isn't known, nor which one was expected", refWords(source.Ref))
	case fetched == "":
		return fmt.Sprintf("which commit of %s it fetched isn't known: its provider didn't say; %s was expected", refWords(source.Ref), expectedWords(source))
	case source.BuiltBy(fetched):
		return fmt.Sprintf("fetched %s, the commit %s named when the check was planned", fetched.Short(), refWords(source.Ref))
	case source.Moved(fetched):
		return fmt.Sprintf("fetched %s, not %s, which %s named when the check was planned", fetched.Short(), expectedWords(source), refWords(source.Ref))
	}
	return fmt.Sprintf("fetched %s of %s, which couldn't be resolved when the check was planned", fetched.Short(), refWords(source.Ref))
}

// UnmetWords say what an unmet target needs: "needs Xcode", or "needs
// Xcode, through libharbor" when a prerequisite is what needs it. A newer
// Xcode than the environment has says what it has, "needs Xcode 26.0
// (Xcode 16.4 there)", where "needs Xcode 26.0, which Tart macOS 15
// hasn't" left the person to find it.
func UnmetWords(unmet model.Unmet) string {
	words := "needs " + string(unmet.Needs)
	switch unmet.Has {
	case "":
	case "none":
		words += " (only the Command Line Tools there)"
	default:
		words += " (Xcode " + unmet.Has + " there)"
	}
	if unmet.Through != "" {
		words += ", through " + string(unmet.Through)
	}
	return words
}

// coverageNamed is how many files a coverage line names before counting
// the rest.
const coverageNamed = 5

// CoverageWords say in one line what an assessment read and checked: the
// new version's files it compared, the patches that apply and those it
// couldn't check, what it set apart, and how it read the source where
// that's said, a Git-fetched port's commits or nothing for one that
// fetches nothing. A comparison that found nothing read as one that
// hadn't looked: rust's said only its package.json, and its Cargo
// manifests and patches went unmentioned (rust 1.99.0, batch 23). "Read
// LICENSE, go.mod" read as an instruction (field testing, 2026-10-02).
// Empty where the comparison recorded no coverage.
func CoverageWords(comparison model.UpstreamComparison) string {
	var read, apart, unused, unported, notes []string
	applied, unchecked := 0, 0
	for _, c := range comparison.Coverage {
		if c.Source != "" && c.Policy != assess.SourceRemoved && c.Policy != assess.SourceUncertain {
			// Where the port fetches more than one archive, each file is
			// named in its own; an archive set apart is named itself.
			c.Path = c.Source + ":" + c.Path
		}
		switch {
		case c.Policy == "read":
			read = append(read, c.Path)
		case c.Policy == "patch-applies":
			applied++
		case c.Policy == "patch-unchecked":
			unchecked++
		case c.Treatment == "set-apart" && c.Policy == "native-library-ports":
			// A crate linking a library MacPorts has no port for is set
			// apart by its lock, which was compared: "not compared:
			// Cargo.lock" beside "compared Cargo.lock" said both (field
			// testing, hyperfine 2.0.0). It's said by the crate.
			unported = append(unported, c.Reason)
		case c.Treatment == "set-apart" && c.System != "":
			unused = append(unused, c.Path+" ("+c.System+")")
		case c.Treatment == "set-apart":
			apart = append(apart, c.Path)
		case c.Treatment == "inspected" && (c.Policy == "" || c.Policy == notCompared) && c.Reason != "":
			notes = append(notes, c.Reason)
		}
	}
	var parts []string
	if len(read) > 0 {
		parts = append(parts, "compared "+namedList(read))
	}
	if applied > 0 {
		parts = append(parts, PatchesApply(applied, 0))
	}
	if unchecked > 0 {
		parts = append(parts, prose.Plural(unchecked, "patch")+" unchecked")
	}
	// What was set apart is said as what it is: field testing read "set
	// apart: meson.build (meson)" as saying nothing (the seventh report).
	if len(unused) > 0 {
		parts = append(parts, "not compared, as the port doesn't build with them: "+namedList(unused))
	}
	if len(apart) > 0 {
		parts = append(parts, "not compared: "+namedList(apart))
	}
	if len(unported) > 0 {
		parts = append(parts, "set apart, with no port to declare: "+namedList(unported))
	}
	parts = append(parts, notes...)
	if len(parts) == 0 {
		return ""
	}
	words := strings.Join(parts, "; ")
	if len(parts) == len(notes) {
		// A note may begin with a port's name, which keeps its case.
		return words
	}
	return strings.ToUpper(words[:1]) + words[1:]
}

// namedList names up to coverageNamed items, and counts the rest. An item
// named more than once, as a workspace's Cargo.lock in each member, is
// named once with how many: serie's read "Cargo.lock, Cargo.lock,
// Cargo.lock, Cargo.lock" (the rc6 full stage, B8).
func namedList(items []string) string {
	counts := map[string]int{}
	var unique []string
	for _, item := range items {
		if counts[item] == 0 {
			unique = append(unique, item)
		}
		counts[item]++
	}
	items = unique
	for i, item := range items {
		if counts[item] > 1 {
			items[i] = fmt.Sprintf("%s (%d)", item, counts[item])
		}
	}
	if len(items) <= coverageNamed {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(items[:coverageNamed], ", "), len(items)-coverageNamed)
}

// PatchesApply says how many patches apply, "6 patches apply", or of how
// many, where others don't.
func PatchesApply(applied, rejected int) string {
	switch {
	case rejected > 0:
		return fmt.Sprintf("%d of %d patches apply", applied, applied+rejected)
	case applied == 1:
		return "1 patch applies"
	}
	return fmt.Sprintf("%d patches apply", applied)
}

// ReleaseSource says where a release was found: "GitHub tag jq-1.8.1",
// "GitHub commit e2a98e1" for a commit pin's, or "from its
// distfiles"; nothing where none says.
func ReleaseSource(release model.Release) string {
	switch {
	case release.Tag != "":
		forge := release.Forge
		switch forge {
		case "github":
			forge = "GitHub"
		case "gitlab":
			forge = "GitLab"
		}
		// A commit pin's release is a commit, not a tag.
		if release.Pinned {
			return strings.TrimSpace(forge + " commit " + model.ObjectID(release.Commit).Short())
		}
		return strings.TrimSpace(forge + " tag " + release.Tag)
	case release.Archive:
		return "from its distfiles"
	}
	return ""
}

// ReleaseWords says where an update's version came from, as status says
// it: "jq 1.8.1, GitHub tag jq-1.8.1 of jqlang/jq at 1a2b3c4".
func ReleaseWords(port string, release model.Release) string {
	words := port + " " + release.Version
	if source := ReleaseSource(release); source != "" {
		words += ", " + source
	}
	if release.Tag != "" && release.Repository != "" {
		words += " of " + release.Repository
	}
	if release.Tag != "" && release.Commit != "" {
		words += " at " + model.ObjectID(release.Commit).Short()
	}
	return words
}

// Ago says how old an observation is, roughly: "just now", "5m ago", "3d
// ago".
func Ago(at time.Time) string {
	elapsed := time.Since(at)
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed.Minutes()))
	case elapsed < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(elapsed.Hours()/24))
}

// AgeWords says an age as a person would: 15 days, 36 hours, 90 minutes.
func AgeWords(age time.Duration) string {
	switch {
	case age >= 24*time.Hour && age%(24*time.Hour) == 0:
		return prose.Plural(int(age/(24*time.Hour)), "day")
	case age >= time.Hour && age%time.Hour == 0:
		return prose.Plural(int(age/time.Hour), "hour")
	}
	return prose.Plural(int(age.Round(time.Minute)/time.Minute), "minute")
}

// Grouped are comparisons that say the same thing, one for each set of
// ports, named together in the order given: a new python port's subports
// each repeated its stub's block (field testing, 2026-10-02: py-mlx-vlm).
func Grouped(comparisons []PortComparison) []PortComparison {
	said := func(c model.UpstreamComparison) string {
		var lines []string
		for _, change := range c.Changes {
			lines = append(lines, change.Message)
		}
		return strings.Join(append(lines, c.Problem, CoverageWords(c)), "\n")
	}
	var grouped []PortComparison
	var keys []string
	for _, found := range comparisons {
		key := said(found.Comparison)
		if i := slices.Index(keys, key); i >= 0 {
			grouped[i].Port += ", " + found.Port
			continue
		}
		keys = append(keys, key)
		grouped = append(grouped, found)
	}
	return grouped
}

// UnderUpstream is a change as it reads under an Upstream heading, which
// already says whose it is: "upstream's LICENSE changed" is "LICENSE
// changed" there. Elsewhere, as in what holds a submission, the words
// stand alone.
func UnderUpstream(change model.UpstreamChange) model.UpstreamChange {
	for _, prefix := range []string{"upstream's ", "upstream: "} {
		if rest, ok := strings.CutPrefix(change.Message, prefix); ok {
			change.Message = rest
			break
		}
	}
	return change
}

// DescribeEnvironment words where a check builds for a person: the provider,
// and the release it builds on by its macOS name, "tart macOS 26 (Tahoe)
// arm64", or its raw platform where the release is unknown, with its
// developer tools where the provider states them.
func DescribeEnvironment(environment model.Environment) string {
	switch environment.DeveloperTools {
	case model.DeveloperToolsXcode:
		return describePlace(environment) + " with Xcode"
	case model.DeveloperToolsCommandLine:
		return describePlace(environment) + " with the Command Line Tools"
	}
	return describePlace(environment)
}

// EnvironmentHeading is an environment's short name, for a column heading
// where its full words are shown elsewhere: its release alone, macOS 26,
// with its architecture where another environment of all shares the
// release, and its provider where all has several. An environment with no
// platform is its provider.
func EnvironmentHeading(environment model.Environment, all []model.Environment) string {
	platform := environment.Platform
	if platform.Version == "" && platform.Architecture == "" {
		return environment.Provider
	}
	var parts []string
	providers := map[string]bool{}
	shared := 0
	for _, other := range all {
		providers[other.Provider] = true
		if other.Provider == environment.Provider && other.Platform.OS == platform.OS && other.Platform.Version == platform.Version {
			shared++
		}
	}
	if len(providers) > 1 || environment.Prefix != "" {
		parts = append(parts, providerPlace(environment))
	}
	parts = append(parts, macos.ReleaseName(platform))
	if shared > 1 && platform.Architecture != "" {
		parts = append(parts, platform.Architecture)
	}
	return strings.Join(parts, " ")
}

// describePlace is an environment's provider and its platform's words
// (macos.Describe), or its provider alone where it has no platform.
func describePlace(environment model.Environment) string {
	platform := environment.Platform
	if platform.Version == "" && platform.Architecture == "" {
		return providerPlace(environment)
	}
	return providerPlace(environment) + " " + macos.Describe(platform)
}

// PrefixNote is what a plan says of a local check's prefix other than
// /opt/local: MacPorts' binary archives are built for /opt/local alone, so
// every dependency builds from source there. Empty for any other
// environment.
func PrefixNote(environment model.Environment) string {
	if environment.Provider != buildenv.Local || environment.Prefix == "" || environment.Prefix == macports.DefaultPrefix {
		return ""
	}
	return environment.Prefix + ": MacPorts' binary archives are built for " + macports.DefaultPrefix + " alone, so its dependencies build from source"
}

// providerPlace is an environment's provider, with the prefix a local
// check builds in, local:/opt/macports-test, as --on names it.
func providerPlace(environment model.Environment) string {
	if environment.Prefix != "" {
		return environment.Provider + ":" + environment.Prefix
	}
	return environment.Provider
}

// providerWords says how a provider built, as a pull request's Tested on
// line says it: a local check names its prefix and how many ports were
// active there, as it built among them.
func providerWords(environment model.Environment, observed model.Observed) string {
	provider := environment.Provider
	switch provider {
	case buildenv.Tart:
		return provider + ": built in a clean VM"
	case buildenv.Local:
		words := provider + ": built in " + cmp.Or(environment.Prefix, "a MacPorts prefix") + " on the author's Mac"
		if observed.ActivePorts > 0 {
			words += " with " + prose.Plural(observed.ActivePorts, "port") + " active"
		}
		return words
	case buildenv.GitHub:
		return provider + ": MacPorts' CI workflow in the author's fork"
	}
	return provider + ": built by the author's own command"
}

// MacPortsBehind is something set up with an older MacPorts than this
// Mac's, which reads its ports: its name as a person reads it, the
// MacPorts it keeps and this Mac's, and the setup that makes it again.
type MacPortsBehind struct {
	Name, Image, Host, Remedy string
}

// OlderMacPorts are the one sentence of what keeps an older MacPorts than
// this Mac's, a line for each pair of versions, in the order they come:
// "macOS 15's image and macOS 26's Xcode image have MacPorts 2.12.6, older
// than this Mac's 2.12.7: <each one's setup>". A check's plan, setup's
// status, and an Xcode image's setup each composed it, with their own
// grouping (the architecture review of 5897a82, R3-7). reads adds why it
// matters to a check, that this Mac's MacPorts reads their ports.
func OlderMacPorts(behind []MacPortsBehind, reads bool) []string {
	type versions struct{ image, host string }
	var order []versions
	grouped := map[versions][]MacPortsBehind{}
	for _, one := range behind {
		key := versions{one.Image, one.Host}
		if _, seen := grouped[key]; !seen {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], one)
	}
	var lines []string
	for _, key := range order {
		var names, remedies []string
		for _, one := range grouped[key] {
			names = append(names, one.Name)
			if one.Remedy != "" {
				remedies = append(remedies, one.Remedy)
			}
		}
		one := len(names) == 1
		line := fmt.Sprintf("%s %s MacPorts %s, older than this Mac's %s", prose.And(names), map[bool]string{true: "has", false: "have"}[one], key.image, key.host)
		if reads {
			line += ", which reads " + map[bool]string{true: "its", false: "their"}[one] + " ports"
		}
		if len(remedies) > 0 {
			line += ": " + strings.Join(remedies, "; ")
		}
		lines = append(lines, line)
	}
	return lines
}

// RemakeWords is the setup that makes a release's Tart image again, its
// Xcode image with --xcode and the Xcode it takes.
func RemakeWords(release macos.Release, xcode bool) string {
	remedy := "dockhand setup tart " + release.Slug + " --rebuild"
	if xcode {
		remedy += " --xcode <folder> (Xcode " + release.Xcode + "'s .xip, or a folder xcodes downloads it into)"
	}
	return remedy
}
