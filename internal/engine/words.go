package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/model"
)

// targetWords is how one target's result in one environment reads, on the
// terminal and in the pull request alike, so the two never word one result
// differently (Design v3 §7): excluded there by the plan, passed, passed
// with its tests failing or timing out where they didn't count, failed at
// a phase, blocked by a failed changed dependency, not run, or could not
// evaluate. Reading says how the tests counted, "advisory" when empty
// (Evidence.testsReading). Accepted marks a failure submit --accept
// acknowledged.
func targetWords(plan model.Plan, target model.PlanTarget, result Cell, reading string, accepted bool) string {
	if reading == "" {
		reading = "advisory"
	}
	switch result.Kind {
	case CellExcluded:
		return "— excluded"
	case CellUnmet:
		return "· not built: " + UnmetWords(result.Unmet)
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
			words += " at " + string(result.Phase)
		}
		if result.Phase == model.PhaseTest && result.Tests == model.TestsTimedOut {
			words += ": tests timed out"
		}
	case model.OutcomeBlocked:
		words = "✗ blocked by a failed changed dependency"
	case model.OutcomeUnevaluated:
		words = "✗ could not evaluate"
	case model.OutcomeNotRun:
		return "· not run"
	default:
		return "· " + string(result.Outcome)
	}
	if accepted {
		words += ", accepted: cause not established"
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
		return short(source.Commit)
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
		return fmt.Sprintf("git.branch is commit %s, %s", short(source.Commit), fetch)
	case source.Commit != "":
		return fmt.Sprintf("%s names %s now, %s", refWords(source.Ref), short(source.Commit), fetch)
	case source.Abbreviation != "":
		return fmt.Sprintf("git.branch abbreviates a commit, %s, %s", source.Abbreviation, fetch)
	}
	return fmt.Sprintf("which commit %s names isn't known (%s); %s", refWords(source.Ref), source.Unresolved, unknown)
}

// movedWords say why a build that fetched another commit than its plan
// expected failed at fetch.
func movedWords(source model.GitSource, fetched model.ObjectID) string {
	return fmt.Sprintf("the source moved: %s named %s when the check was planned, and the build fetched %s; a new check builds what it names now", refWords(source.Ref), expectedWords(source), short(fetched))
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
		return fmt.Sprintf("fetched %s, the commit %s named when the check was planned", short(fetched), refWords(source.Ref))
	case source.Moved(fetched):
		return fmt.Sprintf("fetched %s, not %s, which %s named when the check was planned", short(fetched), expectedWords(source), refWords(source.Ref))
	}
	return fmt.Sprintf("fetched %s of %s, which couldn't be resolved when the check was planned", short(fetched), refWords(source.Ref))
}

// UnmetWords say what an unmet target needs: "needs Xcode", or "needs
// Xcode, through libharbor" when a prerequisite is what needs it.
func UnmetWords(unmet model.Unmet) string {
	words := "needs " + string(unmet.Needs)
	if unmet.Through != "" {
		words += ", through " + string(unmet.Through)
	}
	return words
}

// coverageNamed is how many files a coverage line names before counting
// the rest.
const coverageNamed = 5

// CoverageWords say in one line what an assessment read and checked: the
// new version's files it read, the patches that apply and those it
// couldn't check, and what it set apart. A comparison that found nothing
// read as one that hadn't looked: rust's said only its package.json, and
// its Cargo manifests and patches went unmentioned (rust 1.99.0, batch
// 23). Empty where the comparison recorded no coverage.
func CoverageWords(comparison model.UpstreamComparison) string {
	var read, apart []string
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
		case c.Treatment == "set-apart" && c.System != "":
			apart = append(apart, c.Path+" ("+c.System+")")
		case c.Treatment == "set-apart":
			apart = append(apart, c.Path)
		}
	}
	var parts []string
	if len(read) > 0 {
		parts = append(parts, "read "+namedList(read))
	}
	switch applied {
	case 0:
	case 1:
		parts = append(parts, "1 patch applies")
	default:
		parts = append(parts, fmt.Sprintf("%d patches apply", applied))
	}
	if unchecked > 0 {
		parts = append(parts, plural(unchecked, "patch")+" unchecked")
	}
	if len(apart) > 0 {
		parts = append(parts, "set apart: "+namedList(apart))
	}
	if len(parts) == 0 {
		return ""
	}
	words := strings.Join(parts, "; ")
	return strings.ToUpper(words[:1]) + words[1:]
}

// namedList names up to coverageNamed items, and counts the rest.
func namedList(items []string) string {
	if len(items) <= coverageNamed {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(items[:coverageNamed], ", "), len(items)-coverageNamed)
}

// AgeWords says an age as a person would: 15 days, 36 hours, 90 minutes.
func AgeWords(age time.Duration) string {
	switch {
	case age >= 24*time.Hour && age%(24*time.Hour) == 0:
		return plural(int(age/(24*time.Hour)), "day")
	case age >= time.Hour && age%time.Hour == 0:
		return plural(int(age/time.Hour), "hour")
	}
	return plural(int(age.Round(time.Minute)/time.Minute), "minute")
}
