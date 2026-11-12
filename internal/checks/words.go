package checks

import (
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// Describe names the revision for people: "snapshot 3" or "commit 7e3f1a2".
func Describe(revision model.Revision) string {
	if revision.Kind == model.RevisionSnapshot {
		if revision.Snapshot == 0 {
			// A plan's capture, which only a check records and numbers.
			return "a new snapshot"
		}
		return fmt.Sprintf("snapshot %d", revision.Snapshot)
	}
	return "commit " + revision.Source.Commit.Short()
}

// RefWords names a Git source's ref as a Portfile gives it: "git.branch
// v1.2", or its repository's default branch where it names none.
func RefWords(ref string) string {
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
		return fmt.Sprintf("%s names %s now, %s", RefWords(source.Ref), source.Commit.Short(), fetch)
	case source.Abbreviation != "":
		return fmt.Sprintf("git.branch abbreviates a commit, %s, %s", source.Abbreviation, fetch)
	}
	return fmt.Sprintf("which commit %s names isn't known (%s); %s", RefWords(source.Ref), source.Unresolved, unknown)
}

// movedWords say why a build that fetched another commit than its plan
// expected failed at fetch.
func movedWords(source model.GitSource, fetched model.ObjectID) string {
	return fmt.Sprintf("the source moved: %s named %s when the check was planned, and the build fetched %s; a new check builds what it names now", RefWords(source.Ref), expectedWords(source), fetched.Short())
}

// FetchedWords say what a Git-fetched target's build fetched, for its
// evidence: the commit it was expected to, another, or, where its provider
// couldn't say, that it isn't known.
func FetchedWords(source model.GitSource, fetched model.ObjectID) string {
	switch {
	case fetched == "" && source.Expected() == "":
		return fmt.Sprintf("which commit of %s it fetched isn't known, nor which one was expected", RefWords(source.Ref))
	case fetched == "":
		return fmt.Sprintf("which commit of %s it fetched isn't known: its provider didn't say; %s was expected", RefWords(source.Ref), expectedWords(source))
	case source.AgainstFetched(fetched) == model.SourceSame:
		return fmt.Sprintf("fetched %s, the commit %s named when the check was planned", fetched.Short(), RefWords(source.Ref))
	case source.AgainstFetched(fetched) == model.SourceMoved:
		return fmt.Sprintf("fetched %s, not %s, which %s named when the check was planned", fetched.Short(), expectedWords(source), RefWords(source.Ref))
	}
	return fmt.Sprintf("fetched %s of %s, which couldn't be resolved when the check was planned", fetched.Short(), RefWords(source.Ref))
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

// DescribeEnvironment words where a check builds for a person: the provider,
// and the release it builds on by its macOS name, "tart macOS 26 (Tahoe)
// arm64", or its raw platform where the release is unknown, with its
// developer tools where the provider states them.
func DescribeEnvironment(environment model.Environment) string {
	switch environment.DeveloperTools {
	case model.DeveloperToolsXcode:
		return DescribePlace(environment) + " with Xcode"
	case model.DeveloperToolsCommandLine:
		return DescribePlace(environment) + " with the Command Line Tools"
	}
	return DescribePlace(environment)
}

// DescribePlace is an environment's provider and its platform's words
// (macos.Describe), or its provider alone where it has no platform.
func DescribePlace(environment model.Environment) string {
	platform := environment.Platform
	if platform.Version == "" && platform.Architecture == "" {
		return ProviderPlace(environment)
	}
	return ProviderPlace(environment) + " " + macos.Describe(platform)
}

// ProviderPlace is an environment's provider, with the prefix a local
// check builds in, local:/opt/macports-test, as --on names it.
func ProviderPlace(environment model.Environment) string {
	if environment.Prefix != "" {
		return environment.Provider + ":" + environment.Prefix
	}
	return environment.Provider
}

// UnresolvedWords say why a plan builds nothing: each unresolved target
// and why, "jq: no Portfile; fd: excluded on arm64", or that it builds
// nothing where none is.
func UnresolvedWords(plan model.Plan) string {
	var words []string
	for _, unresolved := range plan.Unresolved {
		words = append(words, unresolved.Target.Name+": "+unresolved.Reason)
	}
	if len(words) == 0 {
		return "it builds nothing"
	}
	return strings.Join(words, "; ")
}

// CarriedWords say how a check's results stand where it reused an earlier
// check's builds over a change that builds nothing differently, carried
// all of one earlier check's builds (model.Carried): the ports judged and
// the options each changed, where one moved from, and lint's: "check-767
// reused check-766's builds (commit 3f2a1c9): since then only omnidiff's
// maintainers changed, which builds nothing differently, and lint ran
// again on this Portfile". It names what changed, never the rule; empty
// where nothing was carried.
func CarriedWords(reusing string, carried []model.Carried) string {
	if len(carried) == 0 {
		return ""
	}
	type judged struct {
		options, declared []string
		from              string
	}
	var ports []string
	byPort := map[string]*judged{}
	var commit model.ObjectID
	var linted []string
	kept := ""
	for _, c := range carried {
		if commit == "" {
			commit = c.Commit
		}
		for _, port := range c.Ports {
			found, ok := byPort[port.Port]
			if !ok {
				found = &judged{}
				byPort[port.Port] = found
				ports = append(ports, port.Port)
			}
			for _, option := range port.Options {
				if !slices.Contains(found.options, option) {
					found.options = append(found.options, option)
				}
			}
			for _, declared := range port.Declared {
				if !slices.Contains(found.declared, declared) {
					found.declared = append(found.declared, declared)
				}
			}
			if port.From != "" {
				found.from = port.From
			}
		}
		if c.Linted && len(c.Ports) > 0 && !slices.Contains(linted, c.Ports[0].Port) {
			linted = append(linted, c.Ports[0].Port)
		}
		if c.LintKept != "" && kept == "" {
			kept = c.LintKept
		}
	}
	var changed, declared, moved, text []string
	for _, port := range ports {
		found := byPort[port]
		// A port judged with nothing named changed text no option reads,
		// as a comment: said so, where the line named nothing (D-T1 at
		// rc4's full stage).
		if len(found.options) == 0 && len(found.declared) == 0 && found.from == "" {
			text = append(text, port+"'s Portfile")
		}
		if len(found.options) > 0 {
			changed = append(changed, port+"'s "+prose.And(found.options))
		}
		if len(found.declared) > 0 {
			declared = append(declared, port+" declares "+prose.And(found.declared))
		}
		if found.from != "" {
			moved = append(moved, port+" moved from "+found.from)
		}
	}
	var parts []string
	if len(changed) > 0 {
		parts = append(parts, prose.And(changed)+" changed")
	}
	if len(declared) > 0 {
		// A declaration of a port the build had active, as tidy writes
		// from a check's link finding.
		parts = append(parts, prose.And(declared)+", active as that build ran")
	}
	if len(moved) > 0 {
		parts = append(parts, prose.And(moved))
	}
	if len(text) > 0 {
		parts = append(parts, "text in "+prose.And(text)+" that no option reads changed")
	}
	since := strings.Join(parts, ", and ")
	words := reusing + " reused " + carried[0].Check + "'s builds"
	if commit != "" {
		words += " (commit " + commit.Short() + ")"
	}
	words += ": since then only " + since + ", which builds nothing differently"
	switch {
	case len(linted) == 1:
		words += ", and lint ran again on this Portfile"
	case len(linted) > 1:
		words += ", and lint ran again on these Portfiles"
	}
	if kept != "" {
		words += "; lint's lines are the earlier Portfile's, since " + kept
	}
	return words
}
