package view

import (
	"fmt"
	"io"
	"path"
	"strconv"
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
		if finding.Ports == 0 {
			return fmt.Sprintf("%s links no port's libraries", finding.Target)
		}
		return fmt.Sprintf("%s links %s, each declared in depends_lib", finding.Target, prose.Plural(finding.Ports, "port"))
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
// and 10): its own libraries whose install names moved, with the ports
// that link it, and what configure no longer or now finds, each marked
// for a look, and the files gone or added, for information; nothing where
// it has none.
func WriteInstalled(out io.Writer, findings []engine.InstallFinding) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintln(out, "Against the earlier build")
	for _, finding := range findings {
		against := ""
		if finding.Check != "" {
			against = " against " + finding.Check
		}
		where := engine.DescribeEnvironment(finding.Environment)
		for _, library := range finding.Libraries {
			words := fmt.Sprintf("%s: %s on %s%s", finding.Target, library.Words(), where, against)
			switch {
			case len(finding.Dependents) > 0:
				words += fmt.Sprintf("; %s link it, which update --revbump-dependents rebuilds", prose.Few(finding.Dependents, 6, "port"))
			default:
				words += "; no port links it"
			}
			fmt.Fprintf(out, "  %s %s\n", Look, words)
		}
		var files []string
		if len(finding.Removed) > 0 {
			files = append(files, "no longer installs "+prose.Few(finding.Removed, 4, "file"))
		}
		if len(finding.Added) > 0 {
			files = append(files, "now installs "+prose.Few(finding.Added, 4, "file"))
		}
		if len(finding.Found)+len(finding.Lost) > 0 {
			var said []string
			if len(finding.Lost) > 0 {
				said = append(said, "configure no longer says "+prose.FewJoined(quotedAll(finding.Lost), 3, "line", ", "))
			}
			if len(finding.Found) > 0 {
				said = append(said, "now says "+prose.FewJoined(quotedAll(finding.Found), 3, "line", ", "))
			}
			fmt.Fprintf(out, "  %s %s: %s on %s%s\n", Look, finding.Target, strings.Join(said, "; "), where, against)
		}
		if len(files) > 0 && finding.Unbumped {
			fmt.Fprintf(out, "  %s %s: %s on %s%s, with neither the version nor the revision raised, so installed copies keep the old files; raise the revision\n", Look, finding.Target, strings.Join(files, "; "), where, against)
		} else if len(files) > 0 {
			fmt.Fprintf(out, "  %s %s: %s on %s%s\n", Unmarked, finding.Target, strings.Join(files, "; "), where, against)
		}
	}
}

// quotedAll are lines quoted, as a sentence names what a log said.
func quotedAll(lines []string) []string {
	quoted := make([]string, len(lines))
	for i, line := range lines {
		quoted[i] = strconv.Quote(line)
	}
	return quoted
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
