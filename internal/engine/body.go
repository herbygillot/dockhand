package engine

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/model"
)

// The MacPorts pull request template's headings
// (macports-ports .github/PULL_REQUEST_TEMPLATE.md).
const (
	descriptionHeading  = "#### Description"
	typesHeading        = "###### Type(s)"
	testedOnHeading     = "###### Tested on"
	verificationHeading = "###### Verification"
)

// PullRequestTypes are the template's Type(s) choices.
var PullRequestTypes = []string{"bugfix", "enhancement", "security fix"}

var cve = regexp.MustCompile(`\bCVE-\d{4}-\d{4,}\b`)

// bodyFacts is what the description can claim, each from evidence.
type bodyFacts struct {
	Commits  []git.HistoryCommit
	Evidence *Evidence
	NoCheck  bool
	Accepted []string
	Types    []string
	// RulesPassed and Squashed come from the commit rules.
	RulesPassed, Squashed bool
	// Searched is true when other open pull requests were looked for;
	// Others are what was found.
	Searched bool
	Others   []forge.PullRequestSummary
	// TestedBinaries and TestedVariants are the person's own statements.
	TestedBinaries, TestedVariants bool
	SkipNotification               bool
}

// pullRequestBody writes the description in the template's sections,
// ticking only what the facts establish.
func pullRequestBody(facts bodyFacts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", descriptionHeading)
	if len(facts.Commits) == 1 {
		if text := commitBody(facts.Commits[0].Message); text != "" {
			fmt.Fprintf(&b, "%s\n\n", text)
		}
	} else {
		fmt.Fprintln(&b, "| Commit | Port | Change |")
		fmt.Fprintln(&b, "| --- | --- | --- |")
		for _, commit := range facts.Commits {
			ports, change, ok := strings.Cut(commit.Subject(), ":")
			if !ok {
				ports, change = "", commit.Subject()
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", short(model.ObjectID(commit.ID)), cell(ports), cell(change))
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintf(&b, "%s\n\n", typesHeading)
	types := slices.Clone(facts.Types)
	for _, commit := range facts.Commits {
		if cve.MatchString(commit.Message) && !slices.Contains(types, "security fix") {
			types = append(types, "security fix")
		}
	}
	for _, kind := range PullRequestTypes {
		fmt.Fprintf(&b, "- [%s] %s\n", tick(slices.Contains(types, kind)), kind)
	}
	b.WriteString("\n")
	b.WriteString(ownedSections(facts))
	return b.String()
}

// ownedSections are the part of the description dockhand keeps up to date:
// Tested on through Verification.
func ownedSections(facts bodyFacts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", testedOnHeading)
	evidence := facts.Evidence
	switch {
	case facts.NoCheck:
		fmt.Fprintln(&b, "Not built locally: submitted with `dockhand submit --no-check`, so MacPorts CI is the only check this change has had.")
	case evidence == nil:
		fmt.Fprintln(&b, "No local check has finished for this commit yet. This is a draft, so MacPorts CI starts early.")
	default:
		for _, environment := range evidence.Plan.Environments {
			platform := environment.Platform
			fmt.Fprintf(&b, "%s %s %s\n", platformName(platform.OS), platform.Version, platform.Architecture)
			fmt.Fprintf(&b, "Developer tools not recorded · %s\n\n", providerWords(environment.Provider))
		}
		fmt.Fprint(&b, "| Port |")
		for _, environment := range evidence.Plan.Environments {
			fmt.Fprintf(&b, " %s %s %s |", environment.Provider, environment.Platform.Version, environment.Platform.Architecture)
		}
		fmt.Fprint(&b, "\n| --- |")
		for range evidence.Plan.Environments {
			fmt.Fprint(&b, " --- |")
		}
		fmt.Fprintln(&b)
		for _, target := range evidence.Targets {
			fmt.Fprintf(&b, "| %s |", target.Target.Target.Name)
			for i, result := range target.Outcomes {
				fmt.Fprintf(&b, " %s |", TargetWords(evidence.Plan, target.Target, evidence.Plan.Environments[i], result, slices.Contains(facts.Accepted, target.Target.Target.Name)))
			}
			fmt.Fprintln(&b)
		}
		checked := evidence.Run.Name()
		if len(evidence.Earlier) > 0 {
			var earlier []string
			for _, run := range evidence.Earlier {
				earlier = append(earlier, run.Name())
			}
			checked += ", with results from " + strings.Join(earlier, " and ") + " for the same files"
		}
		fmt.Fprintf(&b, "\nChecked by dockhand %s.\n", checked)
	}
	fmt.Fprintf(&b, "\n%s\n\nHave you\n\n", verificationHeading)
	built := evidence != nil && !facts.NoCheck && allBuilt(*evidence, facts.Accepted)
	item := func(done bool, text, note string) {
		if note != "" {
			text += " " + note
		}
		fmt.Fprintf(&b, "- [%s] %s\n", tick(done), text)
	}
	item(facts.RulesPassed, "followed our [Commit Message Guidelines](https://trac.macports.org/wiki/CommitMessages)?", "")
	item(facts.Squashed, "squashed and [minimized your commits](https://guide.macports.org/#project.github)?", "")
	others := ""
	if !facts.Searched {
		others = "(dockhand could not search)"
	} else if len(facts.Others) > 0 {
		var links []string
		for _, pr := range facts.Others {
			links = append(links, fmt.Sprintf("#%d", pr.Number))
		}
		others = "(open for the same ports: " + strings.Join(links, ", ") + ")"
	}
	item(facts.Searched && len(facts.Others) == 0, "checked that there aren't other open [pull requests](https://github.com/macports/macports-ports/pulls) for the same change?", others)
	item(citesTickets(facts.Commits), "referenced existing tickets on [Trac](https://trac.macports.org/wiki/Tickets) with full URL in commit message?", "")
	item(built, "checked your Portfile with `port lint`?", "")
	if tests := testsDeclared(evidence); evidence == nil || tests {
		item(built && testsPassed(*evidence), "tried existing tests with `sudo port test`?", "")
	}
	item(built, "tried a full install with `sudo port -vst install`?", installNote(built))
	item(facts.TestedBinaries, "tested basic functionality of all binary files?", "")
	item(facts.TestedVariants, "checked that the Portfile's most important [variants](https://trac.macports.org/wiki/Variants) haven't been broken?", "")
	if facts.SkipNotification {
		fmt.Fprint(&b, "\n[skip notification]\n")
	}
	return b.String()
}

func tick(done bool) string {
	if done {
		return "x"
	}
	return " "
}

func cell(text string) string {
	return strings.ReplaceAll(strings.TrimSpace(text), "|", `\|`)
}

// commitBody is a commit message's text after the subject, without
// dockhand's attribution.
func commitBody(message string) string {
	_, rest, _ := strings.Cut(strings.TrimSpace(message), "\n")
	var kept []string
	for _, line := range strings.Split(strings.TrimSpace(rest), "\n") {
		if !commitmsg.IsAttribution(line) {
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func citesTickets(commits []git.HistoryCommit) bool {
	return slices.ContainsFunc(commits, func(c git.HistoryCommit) bool {
		return strings.Contains(c.Message, "https://trac.macports.org/ticket/")
	})
}

func platformName(os string) string {
	if os == "darwin" || os == "" {
		return "macOS"
	}
	return os
}

func providerWords(provider string) string {
	switch provider {
	case "tart":
		return "tart: built in a clean VM"
	case "prefix":
		return "prefix: built in a MacPorts prefix on the author's Mac"
	case "github":
		return "github: MacPorts' CI workflow in the author's fork"
	}
	return provider + ": built by the author's own command"
}

// allBuilt reports whether every target passed or was accepted.
func allBuilt(evidence Evidence, accepted []string) bool {
	for _, target := range evidence.Failed() {
		if !slices.Contains(accepted, target.Target.Target.Name) {
			return false
		}
	}
	return len(evidence.Targets) > 0
}

func testsDeclared(evidence *Evidence) bool {
	if evidence == nil {
		return false
	}
	for _, target := range evidence.Targets {
		for _, result := range target.Outcomes {
			if result.Tests == model.TestsPassed || result.Tests == model.TestsFailed {
				return true
			}
		}
	}
	return false
}

func testsPassed(evidence Evidence) bool {
	for _, target := range evidence.Targets {
		for _, result := range target.Outcomes {
			if result.Tests == model.TestsFailed {
				return false
			}
		}
	}
	return true
}

func installNote(built bool) string {
	if !built {
		return ""
	}
	return "(dockhand builds from source as MacPorts CI does, without trace mode)"
}

// ownedSpan locates the part of a description dockhand keeps up to date,
// from the Tested on heading to the end.
func ownedSpan(body string) (int, bool) {
	at := strings.Index(body, testedOnHeading)
	return at, at >= 0
}

// mergeBody updates an existing description: dockhand's part is rewritten
// only while it is still exactly what dockhand last wrote there, so a
// person's edits are kept. It reports whether it could.
func mergeBody(existing, lastWritten, fresh string) (string, bool) {
	at, ok := ownedSpan(existing)
	last, lastOK := ownedSpan(lastWritten)
	next, nextOK := ownedSpan(fresh)
	if !ok || !lastOK || !nextOK || normalize(existing[at:]) != normalize(lastWritten[last:]) {
		return existing, false
	}
	return existing[:at] + fresh[next:], true
}

// normalize ignores the line endings GitHub's editor may change.
func normalize(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
}
