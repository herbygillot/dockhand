// Package prdescription is the description of a pull request against
// macports/macports-ports, as MacPorts' template has it
// (.github/PULL_REQUEST_TEMPLATE.md): its headings and Type(s), what each
// part claims, from facts the engine establishes, and how submitting again
// updates a description, keeping what a person edited (the architecture
// review's finding 5). It composes and merges a document without a store,
// a repository, a provider, or the engine: the engine supplies the facts,
// in words where they're its own, observes the description as it stands,
// and publishes it.
package prdescription

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/version"
)

// The MacPorts pull request template's headings.
const (
	descriptionHeading  = "#### Description"
	typesHeading        = "###### Type(s)"
	testedOnHeading     = "###### Tested on"
	verificationHeading = "###### Verification"
)

// types are the template's Type(s) choices.
var types = []string{"bugfix", "enhancement", "security fix"}

// Types are the template's Type(s) choices, in its order.
func Types() []string { return slices.Clone(types) }

// IsType reports one of the template's Type(s) choices.
func IsType(kind string) bool { return slices.Contains(types, kind) }

var cve = regexp.MustCompile(`\bCVE-\d{4}-\d{4,}\b`)

// Facts are what a description can claim, each established by the
// engine: the commits, the ports the branch adds, the person's note and
// Type(s), what the checks found (TestedOn), and the checklist's answers
// (Verification).
type Facts struct {
	Commits  []Commit
	NewPorts []NewPort
	// Note is the person's own note, which the Description gives after
	// what dockhand wrote there.
	Note string
	// Types are the Type(s) the person named; Updated is true when
	// dockhand wrote every commit and one is an update it made, a new
	// release, which the template calls an enhancement unless Types says
	// otherwise. A commit citing a CVE is a security fix.
	Types   []string
	Updated bool
	// TestedOn is what the checks found, and Verification the checklist.
	TestedOn         TestedOn
	Verification     Verification
	SkipNotification bool
	// Version is dockhand's release, which the last line gives; empty
	// where the build doesn't know it.
	Version string
}

// Commit is one commit the pull request carries.
type Commit struct {
	ID, Message string
}

// subject is a commit's first line.
func (c Commit) subject() string {
	subject, _, _ := strings.Cut(strings.TrimSpace(c.Message), "\n")
	return subject
}

// NewPort is a port a branch adds: its name and version, its one line,
// its homepage, and its license in a person's words.
type NewPort struct {
	Name, Version, Description, Homepage, License string
}

// TestedOn is what the checks found: nothing, where the submission has no
// check (NoCheck) or none has finished (Pending); otherwise what each
// environment reported (Reports), and the table of each port's result in
// each environment (Columns, Rows).
type TestedOn struct {
	NoCheck, Pending bool
	Reports          []Report
	Columns          []string
	Rows             []Row
}

// Report is one environment's report: what it observed of itself, its
// architecture and its release and tools as the environment states them,
// for what it didn't report, who built it, in words, and the runs behind
// it.
type Report struct {
	Observed model.Observed
	// Architecture is the environment's, where its report names none;
	// Release is its release and architecture as the environment states
	// them, "macOS 26 (Tahoe) arm64", for a report that names no macOS.
	Architecture, Release string
	Tools                 model.DeveloperTools
	// Provider is who built it, in words: "tart: built in a clean VM".
	Provider string
	Runs     []Run
}

// Run is a provider run behind a report: dockhand's ID, the provider's
// reference, the check it was in, and the check that reused what it
// built.
type Run struct {
	ID, Ref, Check, ReusedIn string
}

// Row is one port's results, a cell for each column.
type Row struct {
	Port  string
	Cells []Cell
}

// Cell is one result in words, and the reason it gave for not wholly
// passing, which a note under the table says.
type Cell struct {
	Words, Reason string
}

// Verification are the checklist's answers: which items the facts tick,
// and what a note beside an item says.
type Verification struct {
	RulesPassed, Squashed bool
	// Searched is true where other open pull requests were looked for,
	// and Others the numbers of those found.
	Searched bool
	Others   []int
	// Built is true where every port passed or was accepted; AsksTests
	// where the tests item is asked, and TestsPassed where it's ticked.
	Built, AsksTests, TestsPassed bool
	// TestedBinaries and Variants are ticked by the person's statements,
	// or Variants by a check of each variant, which VariantsNote says.
	TestedBinaries, Variants bool
	VariantsNote             string
}

// Compose writes the description in the template's sections, ticking only
// what the facts establish.
func Compose(facts Facts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s\n\n", submittedBy, descriptionHeading)
	// A new port is said as its Portfile says it, for a reviewer who has
	// never heard of it (the txt run's finding 4). Its Type(s) stay as
	// they are: MacPorts' automation labels a new Portfile a submission.
	for _, port := range facts.NewPorts {
		fmt.Fprintf(&b, "New port **%s** %s", port.Name, port.Version)
		if port.Description != "" {
			fmt.Fprintf(&b, ": %s", port.Description)
		}
		b.WriteString("\n")
		if port.Homepage != "" {
			fmt.Fprintf(&b, "\n- homepage: %s", port.Homepage)
		}
		if port.License != "" {
			fmt.Fprintf(&b, "\n- license: %s", port.License)
		}
		b.WriteString("\n\n")
	}
	if len(facts.Commits) == 1 {
		if text := commitBody(facts.Commits[0].Message); text != "" {
			fmt.Fprintf(&b, "%s\n\n", text)
		}
	} else {
		fmt.Fprintln(&b, "| Commit | Port | Change |")
		fmt.Fprintln(&b, "| --- | --- | --- |")
		for _, commit := range facts.Commits {
			ports, change, ok := strings.Cut(commit.subject(), ":")
			if !ok {
				ports, change = "", commit.subject()
			}
			id := commit.ID
			if len(id) > 7 {
				id = id[:7]
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", id, escape(ports), escape(change))
		}
		fmt.Fprintln(&b)
	}
	if facts.Note != "" {
		b.WriteString(noteWords(facts.Note))
	}
	ticked := slices.Clone(facts.Types)
	if len(ticked) == 0 && facts.Updated {
		ticked = append(ticked, "enhancement")
	}
	for _, commit := range facts.Commits {
		if cve.MatchString(commit.Message) && !slices.Contains(ticked, "security fix") {
			ticked = append(ticked, "security fix")
		}
	}
	b.WriteString(typesSection(ticked))
	b.WriteString(Owned(facts))
	return b.String()
}

// noteWords is a person's note as the Description gives it, a quote led
// by "Author's note:", so a reviewer tells it from what dockhand wrote. A
// quote also keeps any of its lines from beginning a heading, which would
// end the Description there (sectionSpan), and submitting again would
// read the rest of the note as a part dockhand doesn't write.
func noteWords(note string) string {
	var b strings.Builder
	for i, line := range strings.Split(note, "\n") {
		if i == 0 {
			line = "**Author's note:** " + line
		}
		fmt.Fprintln(&b, strings.TrimRight("> "+line, " \t"))
	}
	b.WriteString("\n")
	return b.String()
}

// typesSection is a description's Type(s), with these ticked.
func typesSection(ticked []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", typesHeading)
	for _, kind := range types {
		fmt.Fprintf(&b, "- [%s] %s\n", tick(slices.Contains(ticked, kind)), kind)
	}
	b.WriteString("\n")
	return b.String()
}

// tickedTypes are the Type(s) a Type(s) part as dockhand writes it ticks.
func tickedTypes(section string) []string {
	var ticked []string
	for _, line := range strings.Split(section, "\n") {
		for _, kind := range types {
			if strings.TrimSpace(line) == "- [x] "+kind {
				ticked = append(ticked, kind)
			}
		}
	}
	return ticked
}

// Owned is the part of the description dockhand keeps up to date: Tested
// on through Verification.
func Owned(facts Facts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", testedOnHeading)
	tested := facts.TestedOn
	switch {
	case tested.NoCheck:
		fmt.Fprintln(&b, "Not built locally: submitted with `dockhand submit --no-check`, so MacPorts CI is the only check this change has had.")
	case tested.Pending:
		fmt.Fprintln(&b, "No local check has finished for this commit yet. This is a draft, so MacPorts CI starts early.")
	default:
		for _, report := range tested.Reports {
			b.WriteString(report.lines())
		}
		fmt.Fprint(&b, "| Port |")
		for _, column := range tested.Columns {
			fmt.Fprintf(&b, " %s |", escape(column))
		}
		fmt.Fprint(&b, "\n| --- |")
		for range tested.Columns {
			fmt.Fprint(&b, " --- |")
		}
		fmt.Fprintln(&b)
		var notes reasonNotes
		for _, row := range tested.Rows {
			fmt.Fprintf(&b, "| %s |", row.Port)
			for i, cell := range row.Cells {
				words := cell.Words
				if cell.Reason != "" && i < len(tested.Columns) {
					words += notes.mark(cell.Reason, row.Port, tested.Columns[i])
				}
				fmt.Fprintf(&b, " %s |", words)
			}
			fmt.Fprintln(&b)
		}
		b.WriteString(notes.String())
	}
	answers := facts.Verification
	fmt.Fprintf(&b, "\n%s\n\nHave you\n\n", verificationHeading)
	item := func(done bool, text, note string) {
		if note != "" {
			text += " " + note
		}
		fmt.Fprintf(&b, "- [%s] %s\n", tick(done), text)
	}
	item(answers.RulesPassed, "followed our [Commit Message Guidelines](https://trac.macports.org/wiki/CommitMessages)?", "")
	item(answers.Squashed, "squashed and [minimized your commits](https://guide.macports.org/#project.github)?", "")
	others := ""
	if !answers.Searched {
		others = "(dockhand could not search)"
	} else if len(answers.Others) > 0 {
		var links []string
		for _, number := range answers.Others {
			links = append(links, fmt.Sprintf("#%d", number))
		}
		others = "(open for the same ports: " + strings.Join(links, ", ") + ")"
	}
	item(answers.Searched && len(answers.Others) == 0, "checked that there aren't other open [pull requests](https://github.com/macports/macports-ports/pulls) for the same change?", others)
	item(citesTickets(facts.Commits), "referenced existing tickets on [Trac](https://trac.macports.org/wiki/Tickets) with full URL in commit message?", "")
	item(answers.Built, "checked your Portfile with `port lint`?", "")
	if answers.AsksTests {
		item(answers.TestsPassed, "tried existing tests with `sudo port test`?", "")
	}
	item(answers.Built, "tried a full install with `sudo port -vst install`?", installNote(answers.Built))
	item(answers.TestedBinaries, "tested basic functionality of all binary files?", "")
	item(answers.Variants, "checked that the Portfile's most important [variants](https://trac.macports.org/wiki/Variants) haven't been broken?", answers.VariantsNote)
	if facts.SkipNotification {
		fmt.Fprint(&b, "\n[skip notification]\n")
	}
	// A comment, which GitHub doesn't show, ends the checklist's list:
	// Markdown would take dockhand's line, a list item after a blank line,
	// for the checklist's last item, and space the checklist out for it.
	fmt.Fprintf(&b, "\n%s\n\n%s\n", listEnd, signature(facts.Version))
	return b.String()
}

// installNote says how dockhand built, beside the install item it ticks.
func installNote(built bool) string {
	if !built {
		return ""
	}
	return "(dockhand builds from source as MacPorts CI does, without trace mode)"
}

// lines are one environment's lines under Tested on, as MacPorts'
// template has them: the macOS version, build, and architecture, then
// Xcode's version and build or the Command Line Tools', as the environment
// reported them, then who built it, and in which runs of which checks. An
// environment of several builders, as MacPorts' workflow has, gives the
// releases they reported, "macOS 14, 15, 26". What it didn't report is
// said by the release's name and the tools the environment stated, and
// never by the Darwin version, which isn't macOS's.
func (r Report) lines() string {
	var b strings.Builder
	observed := r.Observed
	var releases []string
	for _, builder := range observed.Builders {
		if builder.MacOS != "" && !slices.Contains(releases, builder.MacOS) {
			releases = append(releases, builder.MacOS)
		}
	}
	switch {
	case observed.MacOS != "":
		fmt.Fprintln(&b, strings.Join(nonEmpty("macOS", observed.MacOS, observed.Build, cmpOr(observed.Architecture, r.Architecture)), " "))
	case len(releases) > 0:
		fmt.Fprintln(&b, "macOS "+strings.Join(releases, ", "))
	case r.Release != "":
		fmt.Fprintln(&b, r.Release)
	}
	var tools string
	switch {
	case observed.Xcode != "":
		tools = strings.Join(nonEmpty("Xcode", observed.Xcode, observed.XcodeBuild), " ")
	case observed.Tools != "":
		tools = "Command Line Tools " + observed.Tools
	case r.Tools == model.DeveloperToolsXcode:
		tools = "Xcode, its version not recorded"
	case r.Tools == model.DeveloperToolsCommandLine:
		tools = "Command Line Tools, their version not recorded"
	default:
		tools = "Developer tools not recorded"
	}
	if observed.MacPorts != "" {
		tools += " · MacPorts " + observed.MacPorts
	}
	fmt.Fprintf(&b, "%s · %s%s\n\n", tools, r.Provider, runWords(r.Runs))
	return b.String()
}

func nonEmpty(values ...string) []string {
	return slices.DeleteFunc(values, func(value string) bool { return value == "" })
}

// cmpOr is the first of its values that isn't empty.
func cmpOr(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// submittedBy is the description's first line, naming dockhand in bold,
// whose version the last line gives. plainSubmittedBy is the line as
// dockhand wrote it before it was bold, which a description it wrote
// then, and nobody changed, is given in its place.
var (
	submittedBy      = "Submitted by **[dockhand](" + version.ProjectURL + ")**"
	plainSubmittedBy = "Submitted by [dockhand](" + version.ProjectURL + ")"
)

// listEnd ends the Verification checklist before dockhand's last line.
const listEnd = "<!-- dockhand -->"

// signature is the description's last line, dockhand with its version, or
// dockhand alone when the build doesn't know its version.
func signature(tag string) string {
	line := "- [dockhand](" + version.ProjectURL + ")"
	if tag = strings.TrimSpace(tag); tag != "" {
		line += " ver. " + tag
	}
	return line
}

func escape(text string) string {
	return strings.ReplaceAll(strings.TrimSpace(text), "|", `\|`)
}

func tick(done bool) string {
	if done {
		return "x"
	}
	return " "
}

// reasonNotes are the notes under the Tested on table: one for each reason
// a result gave for not wholly passing (Cell.Reason), naming each port and
// environment that gave it, whose cells carry the note's mark. The rust
// run, #35084, read "tests failed (advisory)" on both its releases, and
// nothing said that its bootstrap had panicked before any test ran: a
// reviewer, who can't read the logs on the author's Mac, has only what the
// pull request says. A note rather than the reason in the cell keeps the
// table readable, and a reason given in several places is said once.
type reasonNotes struct {
	reasons []string
	places  [][]notePlace
}

// notePlace is a port that gave a note's reason, and the environments
// where it did.
type notePlace struct {
	target       string
	environments []string
}

// mark records a reason a target gave in an environment, and returns the
// mark its cell carries: ¹ for the first reason, ² for the next.
func (n *reasonNotes) mark(reason, target, environment string) string {
	reason = strings.Join(strings.Fields(reason), " ")
	i := slices.Index(n.reasons, reason)
	if i < 0 {
		n.reasons, n.places = append(n.reasons, reason), append(n.places, nil)
		i = len(n.reasons) - 1
	}
	if j := slices.IndexFunc(n.places[i], func(place notePlace) bool { return place.target == target }); j >= 0 {
		n.places[i][j].environments = append(n.places[i][j].environments, environment)
	} else {
		n.places[i] = append(n.places[i], notePlace{target: target, environments: []string{environment}})
	}
	return superscript(i + 1)
}

// String is the notes, a line each after a blank line, which ends the
// table: "¹ rust on macOS 15, macOS 26: `tests: Failed to test rust:
// command execution failed`". The reason is quoted as code, as the
// provider's words, which Markdown would otherwise read for emphasis or
// HTML.
func (n reasonNotes) String() string {
	if len(n.reasons) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n")
	for i, reason := range n.reasons {
		var where []string
		for _, place := range n.places[i] {
			where = append(where, place.target+" on "+strings.Join(place.environments, ", "))
		}
		fmt.Fprintf(&b, "%s %s: %s\n", superscript(i+1), strings.Join(where, "; "), codeSpan(reason))
	}
	return b.String()
}

// superscript writes a number in superscript digits, a note's mark.
func superscript(number int) string {
	digits := []rune("⁰¹²³⁴⁵⁶⁷⁸⁹")
	var b strings.Builder
	for _, digit := range strconv.Itoa(number) {
		b.WriteRune(digits[digit-'0'])
	}
	return b.String()
}

// codeSpan quotes text as Markdown code, in a run of backticks longer
// than any the text holds, as CommonMark has a code span hold them.
func codeSpan(text string) string {
	fence := "`"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	if len(fence) > 1 {
		return fence + " " + text + " " + fence
	}
	return fence + text + fence
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

// citesTickets reports a commit citing a Trac ticket by its URL.
func citesTickets(commits []Commit) bool {
	return slices.ContainsFunc(commits, func(c Commit) bool {
		return strings.Contains(c.Message, "https://trac.macports.org/ticket/")
	})
}

// runWords name the provider runs behind an environment's results, each
// with the check it was in: "(Run ID: tart_7y62p4sigena6xlr - checked in
// check-11)". A run is named by its provider's own reference where that is
// a link anyone can follow, such as a workflow run's URL, and by dockhand's
// ID otherwise, which the author's dockhand logs finds the evidence by.
func runWords(runs []Run) string {
	var named []string
	for _, run := range runs {
		name := run.ID
		if strings.HasPrefix(run.Ref, "https://") {
			name = run.Ref
		}
		if run.Check != "" {
			name += " - checked in " + run.Check
		}
		if run.ReusedIn != "" {
			name += ", reused in " + run.ReusedIn
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

// Outcome is what submitting again does to a part of an existing
// pull request's description that dockhand writes.
type Outcome string

const (
	// Refreshed is rewritten, and reads differently for it.
	Refreshed Outcome = "refreshed"
	// Current is dockhand's, and already as it would write it.
	Current Outcome = "current"
	// Kept is someone's own: edited since dockhand wrote it, or
	// never dockhand's, and kept as it is.
	Kept Outcome = "kept"
	// Absent is left out of the description, and stays out.
	Absent Outcome = "absent"
)

// Sections are what submitting again does to each part of an existing
// pull request's description that dockhand writes: its Description, its
// Type(s), and everything from Tested on down.
type Sections struct {
	Description, Types, TestedOn Outcome
}

// Merge updates an existing description, and says what it did to each
// part dockhand writes. Each is rewritten only while it is still exactly
// what dockhand last wrote there, so a person's edits are kept: the
// Description, which is the commit's body or the commits' table, the
// Type(s), and everything from Tested on down. The Type(s) only gain ticks
// that way: what dockhand ticked stays ticked, though a person's change
// folded in since means it wouldn't tick it now. Types the person named
// (named) replace the Type(s) however they read, or go before Tested on in
// a description that leaves them out; unnamed, such a description stays
// without them.
func Merge(existing, lastWritten, fresh string, named bool) (string, Sections) {
	body, testedOn := mergeTestedOn(existing, lastWritten, fresh)
	body, types := mergeTypes(body, lastWritten, fresh, named)
	body, description := mergeDescription(body, lastWritten, fresh)
	return submittedFirst(body, lastWritten, fresh, testedOn), Sections{Description: description, Types: types, TestedOn: testedOn}
}

// submittedFirst gives a description dockhand wrote before its first line
// named dockhand the line fresh begins with, where rewriting everything
// from Tested on down took away the last line that named dockhand. One
// that begins otherwise, or whose first line a person took out, stays as
// it is.
func submittedFirst(body, lastWritten, fresh string, testedOn Outcome) string {
	if rest, ok := strings.CutPrefix(body, plainSubmittedBy+"\n"); ok && strings.HasPrefix(lastWritten, plainSubmittedBy+"\n") && strings.HasPrefix(fresh, submittedBy) {
		return submittedBy + "\n" + rest
	}
	switch {
	case testedOn != Refreshed && testedOn != Current:
		return body
	case !strings.HasPrefix(fresh, submittedBy) || strings.HasPrefix(lastWritten, submittedBy) || !strings.HasPrefix(body, descriptionHeading):
		return body
	}
	return submittedBy + "\n\n" + body
}

// mergeDescription rewrites the Description while it is still exactly what
// dockhand last wrote there, so a commit's body written since the pull
// request opened reaches it; one a person edited, or left out, stays so.
func mergeDescription(body, lastWritten, fresh string) (string, Outcome) {
	from, to, found := sectionSpan(body, descriptionHeading)
	if !found {
		return body, Absent
	}
	was, wasEnd, written := sectionSpan(lastWritten, descriptionHeading)
	start, end, ok := sectionSpan(fresh, descriptionHeading)
	if !written || !ok || normalize(body[from:to]) != normalize(lastWritten[was:wasEnd]) {
		return body, Kept
	}
	section := fresh[start:end]
	return body[:from] + section + body[to:], rewritten(body[from:to], section)
}

// rewritten is a part's outcome once dockhand writes it: refreshed, or
// current where its text stays the same.
func rewritten(was, is string) Outcome {
	if normalize(was) == normalize(is) {
		return Current
	}
	return Refreshed
}

func mergeTestedOn(existing, lastWritten, fresh string) (string, Outcome) {
	at, ok := ownedSpan(existing)
	if !ok {
		return existing, Absent
	}
	last, lastOK := ownedSpan(lastWritten)
	next, nextOK := ownedSpan(fresh)
	if !lastOK || !nextOK || normalize(existing[at:]) != normalize(lastWritten[last:]) {
		return existing, Kept
	}
	return existing[:at] + fresh[next:], rewritten(existing[at:], fresh[next:])
}

func mergeTypes(body, lastWritten, fresh string, named bool) (string, Outcome) {
	from, to, found := typesSpan(body)
	start, end, ok := typesSpan(fresh)
	switch {
	case !ok && found:
		return body, Kept
	case !ok:
		return body, Absent
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
		return body, Kept
	}
	if at, owned := ownedSpan(body); owned && named {
		return body[:at] + types + body[at:], Refreshed
	}
	return body, Absent
}

// normalize ignores the line endings GitHub's editor may change.
func normalize(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
}
