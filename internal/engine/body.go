package engine

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/version"
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
	// Updated is true when dockhand wrote every commit and one is an
	// update it made: a new release, which the template calls an
	// enhancement unless Types says otherwise.
	Updated bool
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
	types := slices.Clone(facts.Types)
	if len(types) == 0 && facts.Updated {
		types = append(types, "enhancement")
	}
	for _, commit := range facts.Commits {
		if cve.MatchString(commit.Message) && !slices.Contains(types, "security fix") {
			types = append(types, "security fix")
		}
	}
	b.WriteString(typesSection(types))
	b.WriteString(ownedSections(facts))
	return b.String()
}

// typesSection is a description's Type(s), with these ticked.
func typesSection(types []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", typesHeading)
	for _, kind := range PullRequestTypes {
		fmt.Fprintf(&b, "- [%s] %s\n", tick(slices.Contains(types, kind)), kind)
	}
	b.WriteString("\n")
	return b.String()
}

// tickedTypes are the Type(s) a Type(s) part as dockhand writes it ticks.
func tickedTypes(section string) []string {
	var ticked []string
	for _, line := range strings.Split(section, "\n") {
		for _, kind := range PullRequestTypes {
			if strings.TrimSpace(line) == "- [x] "+kind {
				ticked = append(ticked, kind)
			}
		}
	}
	return ticked
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
		checks := evidence.Checks()
		// An environment where nothing was built or reused, as one where
		// every port is excluded, has no report, and isn't named: it
		// wasn't tested, and the table says why.
		for i, environment := range evidence.Plan.Environments {
			for _, observation := range evidence.Observations(i) {
				built, reusedIn := evidence.Built(i, observation.Runs)
				b.WriteString(testedOn(environment, observation.Observed, built, checks, reusedIn))
			}
		}
		fmt.Fprint(&b, "| Port |")
		for _, environment := range evidence.Plan.Environments {
			fmt.Fprintf(&b, " %s |", cell(EnvironmentHeading(environment, evidence.Plan.Environments)))
		}
		fmt.Fprint(&b, "\n| --- |")
		for range evidence.Plan.Environments {
			fmt.Fprint(&b, " --- |")
		}
		fmt.Fprintln(&b)
		for _, target := range evidence.Targets {
			fmt.Fprintf(&b, "| %s |", target.Target.Target.Name)
			for i := range target.Outcomes {
				fmt.Fprintf(&b, " %s |", evidence.Words(target, i, slices.Contains(facts.Accepted, target.Target.Target.Name)))
			}
			fmt.Fprintln(&b)
		}
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
	fmt.Fprintf(&b, "\n%s\n", signature(version.Current().Tag()))
	return b.String()
}

// signature is the description's last line, naming dockhand, in bold,
// and its version, in parentheses, or dockhand alone when the build
// doesn't know its version.
func signature(tag string) string {
	line := "Submitted by **[dockhand](" + version.ProjectURL + ")**"
	if tag = strings.TrimSpace(tag); tag != "" {
		line += " (ver. " + tag + ")"
	}
	return line
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

// dockhandUpdate reports whether every commit carries dockhand's
// Generated-By line, which tidy writes only on a port's commit made of
// dockhand's own edits, and one of them is an update dockhand recorded.
func dockhandUpdate(commits []git.HistoryCommit, edits []model.Edit) bool {
	updated := false
	for _, commit := range commits {
		_, rest, _ := strings.Cut(strings.TrimSpace(commit.Message), "\n")
		_, trailers := splitTrailers(strings.TrimSpace(rest))
		if !slices.ContainsFunc(trailers, commitmsg.IsAttribution) {
			return false
		}
		updated = updated || slices.ContainsFunc(edits, func(edit model.Edit) bool {
			return edit.Kind == model.EditUpdate && edit.Subject == commit.Subject()
		})
	}
	return updated
}

func citesTickets(commits []git.HistoryCommit) bool {
	return slices.ContainsFunc(commits, func(c git.HistoryCommit) bool {
		return strings.Contains(c.Message, "https://trac.macports.org/ticket/")
	})
}

// testedOn is one environment's lines under Tested on, as MacPorts'
// template has them: the macOS version, build, and architecture, then
// Xcode's version and build or the Command Line Tools', as the environment
// reported them, then who built it, and in which runs of which checks. What
// it didn't report is said by the release's name and the tools the
// environment stated, and never by the Darwin version, which isn't macOS's.
func testedOn(environment model.Environment, observed model.Observed, runs []model.GuestExecution, checks map[model.RunID]string, reusedIn map[model.ExecutionID]string) string {
	var b strings.Builder
	platform := environment.Platform
	switch {
	case observed.MacOS != "":
		fmt.Fprintln(&b, strings.Join(nonEmpty("macOS", observed.MacOS, observed.Build, firstOf(observed.Architecture, platform.Architecture)), " "))
	case platform != (model.Platform{}):
		fmt.Fprintln(&b, strings.TrimPrefix(describePlace(model.Environment{Platform: platform}), " "))
	}
	var tools string
	switch {
	case observed.Xcode != "":
		tools = strings.Join(nonEmpty("Xcode", observed.Xcode, observed.XcodeBuild), " ")
	case observed.Tools != "":
		tools = "Command Line Tools " + observed.Tools
	case environment.DeveloperTools == model.DeveloperToolsXcode:
		tools = "Xcode, its version not recorded"
	case environment.DeveloperTools == model.DeveloperToolsCommandLine:
		tools = "Command Line Tools, their version not recorded"
	default:
		tools = "Developer tools not recorded"
	}
	if observed.MacPorts != "" {
		tools += " · MacPorts " + observed.MacPorts
	}
	fmt.Fprintf(&b, "%s · %s%s\n\n", tools, providerWords(environment.Provider), runWords(runs, checks, reusedIn))
	return b.String()
}

// runWords name the provider runs behind an environment's results, each
// with the check it was in: "(Run ID: tart_7y62p4sigena6xlr - checked in
// check-11)". A run is named by its provider's own reference where that is
// a link anyone can follow, such as a workflow run's URL, and by dockhand's
// ID otherwise, which the author's dockhand logs finds the evidence by.
func runWords(runs []model.GuestExecution, checks map[model.RunID]string, reusedIn map[model.ExecutionID]string) string {
	var named []string
	for _, run := range runs {
		name := string(run.ID)
		if strings.HasPrefix(run.ProviderRef, "https://") {
			name = run.ProviderRef
		}
		if check := checks[run.Run]; check != "" {
			name += " - checked in " + check
		}
		if check := reusedIn[run.ID]; check != "" {
			name += ", reused in " + check
		}
		if !slices.Contains(named, name) {
			named = append(named, name)
		}
	}
	switch len(named) {
	case 0:
		return ""
	case 1:
		return " (Run ID: " + named[0] + ")"
	}
	return " (Run IDs: " + strings.Join(named, "; ") + ")"
}

func nonEmpty(values ...string) []string {
	return slices.DeleteFunc(values, func(value string) bool { return value == "" })
}

func firstOf(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func providerWords(provider string) string {
	switch provider {
	case buildenv.Tart:
		return provider + ": built in a clean VM"
	case buildenv.Prefix:
		return provider + ": built in a MacPorts prefix on the author's Mac"
	case buildenv.GitHub:
		return provider + ": MacPorts' CI workflow in the author's fork"
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
			switch result.Tests {
			case model.TestsPassed, model.TestsFailed, model.TestsTimedOut:
				return true
			}
		}
	}
	return false
}

func testsPassed(evidence Evidence) bool {
	for _, target := range evidence.Targets {
		for _, result := range target.Outcomes {
			if result.Tests == model.TestsFailed || result.Tests == model.TestsTimedOut {
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

// typesSpan locates a description's Type(s). A description may leave them
// out, as one a person edited, or wrote from another template, can.
func typesSpan(body string) (int, int, bool) { return sectionSpan(body, typesHeading) }

// sectionSpan locates a part of a description, from its heading to the
// next heading.
func sectionSpan(body, heading string) (int, int, bool) {
	start := strings.Index(body, heading)
	if start < 0 {
		return 0, 0, false
	}
	end := len(body)
	if next := strings.Index(body[start+len(heading):], "\n#"); next >= 0 {
		end = start + len(heading) + next + 1
	}
	return start, end, true
}

// SectionOutcome is what submitting again does to a part of an existing
// pull request's description that dockhand writes.
type SectionOutcome string

const (
	// SectionRefreshed is rewritten, and reads differently for it.
	SectionRefreshed SectionOutcome = "refreshed"
	// SectionCurrent is dockhand's, and already as it would write it.
	SectionCurrent SectionOutcome = "current"
	// SectionKept is someone's own: edited since dockhand wrote it, or
	// never dockhand's, and kept as it is.
	SectionKept SectionOutcome = "kept"
	// SectionAbsent is left out of the description, and stays out.
	SectionAbsent SectionOutcome = "absent"
)

// DescriptionSections are what submitting again does to each part of an
// existing pull request's description that dockhand writes: its
// Description, its Type(s), and everything from Tested on down.
type DescriptionSections struct {
	Description, Types, TestedOn SectionOutcome
}

// mergeBody updates an existing description, and says what it did to each
// part dockhand writes. Each is rewritten only while it is still exactly
// what dockhand last wrote there, so a person's edits are kept: the
// Description, which is the commit's body or the commits' table, the
// Type(s), and everything from Tested on down. The Type(s) only gain ticks
// that way: what dockhand ticked stays ticked, though a person's change
// folded in since means it wouldn't tick it now. Types the person named
// (named) replace the Type(s) however they read, or go before Tested on in
// a description that leaves them out; unnamed, such a description stays
// without them.
func mergeBody(existing, lastWritten, fresh string, named bool) (string, DescriptionSections) {
	body, testedOn := mergeTestedOn(existing, lastWritten, fresh)
	body, types := mergeTypes(body, lastWritten, fresh, named)
	body, description := mergeDescription(body, lastWritten, fresh)
	return body, DescriptionSections{Description: description, Types: types, TestedOn: testedOn}
}

// mergeDescription rewrites the Description while it is still exactly what
// dockhand last wrote there, so a commit's body written since the pull
// request opened reaches it; one a person edited, or left out, stays so.
func mergeDescription(body, lastWritten, fresh string) (string, SectionOutcome) {
	from, to, found := sectionSpan(body, descriptionHeading)
	if !found {
		return body, SectionAbsent
	}
	was, wasEnd, written := sectionSpan(lastWritten, descriptionHeading)
	start, end, ok := sectionSpan(fresh, descriptionHeading)
	if !written || !ok || normalize(body[from:to]) != normalize(lastWritten[was:wasEnd]) {
		return body, SectionKept
	}
	section := fresh[start:end]
	return body[:from] + section + body[to:], rewritten(body[from:to], section)
}

// rewritten is a part's outcome once dockhand writes it: refreshed, or
// current where its text stays the same.
func rewritten(was, is string) SectionOutcome {
	if normalize(was) == normalize(is) {
		return SectionCurrent
	}
	return SectionRefreshed
}

func mergeTestedOn(existing, lastWritten, fresh string) (string, SectionOutcome) {
	at, ok := ownedSpan(existing)
	if !ok {
		return existing, SectionAbsent
	}
	last, lastOK := ownedSpan(lastWritten)
	next, nextOK := ownedSpan(fresh)
	if !lastOK || !nextOK || normalize(existing[at:]) != normalize(lastWritten[last:]) {
		return existing, SectionKept
	}
	return existing[:at] + fresh[next:], rewritten(existing[at:], fresh[next:])
}

func mergeTypes(body, lastWritten, fresh string, named bool) (string, SectionOutcome) {
	from, to, found := typesSpan(body)
	start, end, ok := typesSpan(fresh)
	switch {
	case !ok && found:
		return body, SectionKept
	case !ok:
		return body, SectionAbsent
	}
	types := fresh[start:end]
	if found {
		was, wasEnd, written := typesSpan(lastWritten)
		switch {
		case named:
			return body[:from] + types + body[to:], rewritten(body[from:to], types)
		case written && normalize(body[from:to]) == normalize(lastWritten[was:wasEnd]):
			section := typesSection(append(tickedTypes(body[from:to]), tickedTypes(types)...))
			return body[:from] + section + body[to:], rewritten(body[from:to], section)
		}
		return body, SectionKept
	}
	if at, owned := ownedSpan(body); owned && named {
		return body[:at] + types + body[at:], SectionRefreshed
	}
	return body, SectionAbsent
}

// normalize ignores the line endings GitHub's editor may change.
func normalize(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
}
