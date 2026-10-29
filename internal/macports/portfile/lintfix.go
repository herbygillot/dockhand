package portfile

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// The lint findings dockhand fixes itself, on the lines a change adds or
// alters, as it writes the modeline (the person's "we should have
// Dockhand do those", 2026-10-27; the review survey's §12 and §9): each is
// what `port lint` says of a line, fixed the one way it can be.
const (
	// FixTrailingSpace is trailing spaces and tabs, which lint --nitpick
	// warns of.
	FixTrailingSpace = "trailing-whitespace"
	// FixTabs is leading tabs under MacPorts' modeline, which says tabs
	// are expanded.
	FixTabs = "tabs"
	// FixContinuation is a `\` that carries a command into an unindented
	// next line naming an option or a command, which it swallows.
	FixContinuation = "continuation"
	// FixOpenMaintainer is `maintainers openmaintainer` alone, which lint
	// says is an error: it's nomaintainer.
	FixOpenMaintainer = "openmaintainer"
)

// A LintFix is one kind of fix FixTouched made, and the lines it made it
// on, counted from 1. Evaluates says it changes what the Portfile
// evaluates to: a continuation dropped or a maintainers line always does,
// and whitespace does where it's inside a value, as a braced description
// over lines, and nowhere else; what's kept of an evaluation of the
// files before rests on it (the architecture thread on W5, 2026-10-29).
type LintFix struct {
	Kind      string
	Lines     []int
	Evaluates bool
}

// Words say the fix, as an edit's report says it.
func (f LintFix) Words() string {
	lines := fmt.Sprintf("line %d", f.Lines[0])
	if len(f.Lines) > 1 {
		lines = "lines " + prose.And(numbers(f.Lines))
	}
	switch f.Kind {
	case FixTrailingSpace:
		return "removed trailing whitespace on " + lines
	case FixTabs:
		return "indented " + lines + " with spaces, as the modeline says"
	case FixContinuation:
		return "dropped the \\ ending " + lines + ", which carried the next line's option into its command"
	case FixOpenMaintainer:
		return "made openmaintainer alone nomaintainer on " + lines
	}
	return f.Kind + " on " + lines
}

func numbers(lines []int) []string {
	words := make([]string, len(lines))
	for i, line := range lines {
		words[i] = fmt.Sprint(line)
	}
	return words
}

var openMaintainer = regexp.MustCompile(`^(\s*maintainers\s+)\{?openmaintainer(?:@macports\.org)?\}?(\s*)$`)

// FixTouched fixes, on the lines after adds or alters since before, the
// lint findings dockhand fixes itself: trailing whitespace goes; leading
// tabs become spaces, four to a stop, where line 1 is MacPorts' modeline
// (a maintainer's modeline that sets tabs keeps them, as WithModeline
// leaves it); a `\` that carries a command into an unindented next line
// naming an option or a command is dropped, where either line is touched
// or the two weren't neighbours before; and `maintainers openmaintainer`
// alone becomes nomaintainer. Lines the change didn't touch stay as they
// are, and no line is added or removed. fixes is nil where nothing was
// fixed.
func FixTouched(before, after []byte) (_ []byte, fixes []LintFix) {
	old, lines := splitLines(before), splitLines(after)
	from := matchLines(old, lines)
	touched := func(i int) bool { return from[i] < 0 }
	found := map[string][]int{}
	fix := func(kind string, i int) {
		if !slices.Contains(found[kind], i+1) {
			found[kind] = append(found[kind], i+1)
		}
	}
	// The continuations first, on the text as it stands, since the parse
	// gives their places.
	for _, i := range swallowing(after) {
		if touched(i) || touched(i+1) || from[i+1] != from[i]+1 {
			lines[i] = strings.TrimRight(strings.TrimSuffix(strings.TrimRight(lines[i], " \t"), "\\"), " \t")
			fix(FixContinuation, i)
		}
	}
	expand := len(lines) > 0 && isModeline(lines[0]) && !conflicts(lines[0])
	// Whitespace inside a value is the value's: a fix of it there
	// changes what the Portfile evaluates to.
	values, parsed := valueSpans(after)
	starts := lineStarts(after)
	inValue := func(at int) bool {
		return !parsed || slices.ContainsFunc(values, func(span textedit.Span) bool { return span.Start < at && at < span.End })
	}
	evaluates := map[string]bool{FixContinuation: true, FixOpenMaintainer: true}
	for i, line := range lines {
		if !touched(i) {
			continue
		}
		if trimmed := strings.TrimRight(line, " \t"); trimmed != line && !escapedSpace(line, trimmed) {
			line = trimmed
			fix(FixTrailingSpace, i)
			evaluates[FixTrailingSpace] = evaluates[FixTrailingSpace] || inValue(starts[i]+len(lines[i]))
		}
		if expand {
			if indented := expandTabs(line); indented != line {
				line = indented
				fix(FixTabs, i)
				evaluates[FixTabs] = evaluates[FixTabs] || inValue(starts[i])
			}
		}
		if m := openMaintainer.FindStringSubmatch(line); m != nil {
			line = m[1] + "nomaintainer"
			fix(FixOpenMaintainer, i)
		}
		lines[i] = line
	}
	if len(found) == 0 {
		return after, nil
	}
	for _, kind := range []string{FixTrailingSpace, FixTabs, FixContinuation, FixOpenMaintainer} {
		if len(found[kind]) > 0 {
			slices.Sort(found[kind])
			fixes = append(fixes, LintFix{Kind: kind, Lines: found[kind], Evaluates: evaluates[kind]})
		}
	}
	return []byte(strings.Join(lines, "\n")), fixes
}

// lineStarts are the offsets each of a text's lines starts at, as
// splitLines counts them.
func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// valueSpans are the spans of a Portfile's words over more than one line
// that are values, not a body MacPorts runs as a script: a braced
// description's, a notes block's. False where the source doesn't parse,
// so nothing can be said to be outside one.
func valueSpans(src []byte) ([]textedit.Span, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, false
	}
	var spans []textedit.Span
	commands(src, script, false, func(cmd syntax.Command, _ bool) {
		scripts := bodies(src, cmd)
		for _, word := range cmd.Words {
			if slices.ContainsFunc(scripts, func(s *syntax.Script) bool { return word.Span.Start <= s.Span.Start && s.Span.End <= word.Span.End }) {
				continue
			}
			if bytes.IndexByte(src[word.Span.Start:word.Span.End], '\n') >= 0 {
				spans = append(spans, word.Span)
			}
		}
	})
	return spans, true
}

// splitLines is a text's lines, the last one empty where it ends in a
// newline, so joining them gives the text back.
func splitLines(src []byte) []string {
	if len(src) == 0 {
		return nil
	}
	return strings.Split(string(src), "\n")
}

// escapedSpace reports whether the whitespace trimmed from a line follows
// a backslash that escapes it, which trimming would make a continuation.
func escapedSpace(line, trimmed string) bool {
	slashes := len(trimmed) - len(strings.TrimRight(trimmed, "\\"))
	return slashes%2 == 1 && len(trimmed) < len(line)
}

// expandTabs is a line with the tabs in its leading whitespace made spaces,
// to the next stop of four.
func expandTabs(line string) string {
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	if !strings.Contains(line[:indent], "\t") {
		return line
	}
	column := 0
	for _, c := range line[:indent] {
		if c == '\t' {
			column += 4 - column%4
		} else {
			column++
		}
	}
	return strings.Repeat(" ", column) + line[indent:]
}

// matchLines pairs each line of after with the line of before it keeps,
// by their longest common subsequence: the index in before, or -1 for a
// line the change adds or alters.
func matchLines(before, after []string) []int {
	from, _ := textedit.Match(len(before), len(after), func(i, j int) bool { return before[i] == after[j] })
	return from
}

// commandWord is a word that names an option or a command at a line's
// start: a lower-case name, its parts joined by dots, dashes, or
// underscores, as depends_lib-append or configure.args, or the two that
// start capitalized.
var commandWord = regexp.MustCompile(`^(?:[a-z][a-z0-9_]*(?:[.-][a-z0-9_]+)*|PortGroup|PortSystem)$`)

// swallowing are the lines, counted from 0, whose `\` carries a command
// into an unindented next line that names an option or a command: a word
// of the command, not its first, at the start of a line, which only a
// backslash-newline before it can put there. The bodies MacPorts runs are
// read too, as a subport's or an if's.
func swallowing(src []byte) []int {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil
	}
	var found []int
	commands(src, script, false, func(cmd syntax.Command, _ bool) {
		for _, word := range cmd.Words[1:] {
			start := word.Span.Start
			if start == 0 || src[start-1] != '\n' {
				continue
			}
			if literal, ok := word.Literal(src); ok && commandWord.MatchString(literal) {
				found = append(found, strings.Count(string(src[:start]), "\n")-1)
			}
		}
	})
	return found
}
