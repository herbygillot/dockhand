package view

import (
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// StageLine is a stage's one line at the default level, under its
// change's heading (term.Staged): "  ✓ committed  jq: update to 1.8.1".
func StageLine(mark, stage, words string) string {
	return fmt.Sprintf("  %s %-10s %s", mark, stage, words)
}

// UpdateHeading is a version update's heading over its stage lines, the
// port as the person named it: "jq 1.7.1 → 1.8.1 (GitHub tag jq-1.8.1),
// on branch jq-1.8.1".
func UpdateHeading(named string, update engine.Update, branch model.Branch) string {
	source := ""
	if update.Release != nil && engine.ReleaseSource(*update.Release) != "" {
		source = " (" + engine.ReleaseSource(*update.Release) + ")"
	}
	return fmt.Sprintf("%s %s → %s%s, on branch %s", named, update.Before, update.After, source, branch.ShortName())
}

// PassedOn says where a check that passed built, by the environments'
// headings: "passed on macOS 15 and macOS 26".
func PassedOn(checked engine.Evidence) string {
	var on []string
	for _, i := range checked.Checked() {
		on = append(on, engine.EnvironmentHeading(checked.Plan.Environments[i], checked.Plan.Environments))
	}
	if len(on) == 0 {
		return "passed"
	}
	return "passed on " + prose.And(on)
}

// Where is a branch's directory after its name, " in ~/Source/…", or
// nothing where the command runs in it.
func Where(places config.Places, dir string) string {
	if dir == "" || places.Here(dir) {
		return ""
	}
	return " in " + places.Tilde(dir)
}

// LinkWords say a link finding (evidence.LinkFinding) as a notice, its
// mark !, holding nothing: "harbor links libonig.5.dylib from oniguruma
// (and 1 more of its libraries), which it doesn't declare in depends_lib";
// or, where the links couldn't be read, so nothing is said of them; or,
// where each is declared, how many ports they map to.
func LinkWords(finding engine.LinkFinding) string {
	if finding.Clean {
		switch len(finding.Ports) {
		case 0:
			return fmt.Sprintf("%s links no other port's libraries", finding.Target)
		case 1:
			return fmt.Sprintf("%s links %s's libraries, declared in depends_lib", finding.Target, finding.Ports[0])
		}
		return fmt.Sprintf("%s links %s's libraries, each declared in depends_lib", finding.Target, prose.And(finding.Ports))
	}
	if finding.Unread {
		words := fmt.Sprintf("couldn't read what %s's build links, so whether it declares them isn't said", finding.Target)
		if finding.Reason != "" {
			words = fmt.Sprintf("couldn't read what %s's build links: %s", finding.Target, finding.Reason)
		}
		return words
	}
	more := ""
	if finding.Libraries > 1 {
		more = fmt.Sprintf(" (and %s of its libraries)", prose.Plural(finding.Libraries-1, "more"))
	}
	// Where it doesn't, since a port's dependencies can differ by
	// environment (C8-2).
	where := ""
	if len(finding.Environments) > 0 {
		var on []string
		for _, environment := range finding.Environments {
			on = append(on, engine.DescribeEnvironment(environment))
		}
		where = " on " + prose.And(on)
	}
	return fmt.Sprintf("%s links %s from %s%s, which it doesn't declare in depends_lib%s", finding.Target, path.Base(finding.Library), finding.Port, more, where)
}

// LinkLine is one line of a check's link findings, its mark and words.
type LinkLine struct{ Mark, Words string }

// LinkLines are a check's link findings as lines, the targets whose links
// couldn't be read for one reason folded into one, "couldn't read what 5
// targets link (…): the guest couldn't read them", a line each only where
// the reasons differ: status said five such lines with no reason (the
// exiv2 session's notes, 2026-10-24).
func LinkLines(findings []engine.LinkFinding) []LinkLine {
	var lines []LinkLine
	unread := map[string][]string{}
	at := map[string]int{}
	for _, finding := range findings {
		if !finding.Unread {
			lines = append(lines, LinkLine{LinkMark(finding), LinkWords(finding)})
			continue
		}
		// A reason's line stands where its first target's would.
		if _, seen := at[finding.Reason]; !seen {
			at[finding.Reason] = len(lines)
			lines = append(lines, LinkLine{})
		}
		unread[finding.Reason] = append(unread[finding.Reason], string(finding.Target))
	}
	for reason, i := range at {
		targets := unread[reason]
		if len(targets) == 1 {
			lines[i] = LinkLine{Unmarked, LinkWords(engine.LinkFinding{Target: model.TargetID(targets[0]), Unread: true, Reason: reason})}
			continue
		}
		named := strings.Join(targets, ", ")
		if len(targets) > 4 {
			named = fmt.Sprintf("%s, and %d more", strings.Join(targets[:4], ", "), len(targets)-4)
		}
		words := fmt.Sprintf("couldn't read what %d targets link (%s)", len(targets), named)
		if reason == "" {
			words += ", so whether they declare them isn't said"
		} else {
			words += ": " + reason
		}
		lines[i] = LinkLine{Unmarked, words}
	}
	return lines
}

// LinkMark is a link finding's mark: !, a notice, where a port is linked
// and not declared, and none otherwise.
func LinkMark(finding engine.LinkFinding) string {
	if finding.Unread || finding.Clean {
		return Unmarked
	}
	return Look
}

// WriteInstalled says what a check's builds installed and configure found
// against the port's previous result in each environment (Then's items 9
// and 10), a line each under a heading (InstalledLines); nothing where it
// has none.
func WriteInstalled(out io.Writer, findings []engine.InstallFinding) {
	lines := InstalledLines(findings)
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(out, "Against the earlier build")
	for _, line := range lines {
		fmt.Fprintf(out, "  %s %s\n", line.Mark, line.Words)
	}
}

// InstalledLines are a check's install findings as status and the report
// say them (engine.InstallFinding.Lines), each marked for a look where it
// asks one.
func InstalledLines(findings []engine.InstallFinding) []LinkLine {
	var lines []LinkLine
	for _, finding := range findings {
		for _, line := range finding.Lines(nil) {
			mark := Unmarked
			if line.Look {
				mark = Look
			}
			lines = append(lines, LinkLine{mark, line.Words})
		}
	}
	return lines
}

// LintLines are what lint warned of, a line for each target's warning in
// an environment: a new port's marked ✗, as they hold submit, an update's
// a notice, and a new port lint didn't nitpick said so (the docs dig's
// I13).
func LintLines(findings []engine.LintFinding) []LinkLine {
	var lines []LinkLine
	for _, finding := range findings {
		on := engine.DescribeEnvironment(finding.Environment)
		mark := Look
		if finding.Holds {
			mark = Broken
		}
		for _, said := range finding.Lines {
			lines = append(lines, LinkLine{mark, fmt.Sprintf("%s: %s on %s", finding.Target, said, on)})
		}
		if finding.NotNitpicked {
			lines = append(lines, LinkLine{Unmarked, fmt.Sprintf("%s: a new port, linted without --nitpick on %s, so its nitpicks weren't read", finding.Target, on)})
		}
	}
	return lines
}

// WriteLint says what lint warned of under a heading; nothing where it
// warned of nothing.
func WriteLint(out io.Writer, findings []engine.LintFinding) {
	lines := LintLines(findings)
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(out, "Lint")
	for _, line := range lines {
		fmt.Fprintf(out, "  %s %s\n", line.Mark, line.Words)
	}
}

// WriteLinks says a check's link findings, each a notice under a heading;
// nothing where it has none.
func WriteLinks(out io.Writer, findings []engine.LinkFinding) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintln(out, "Links")
	for _, line := range LinkLines(findings) {
		fmt.Fprintf(out, "  %s %s\n", line.Mark, line.Words)
	}
}
