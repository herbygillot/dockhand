package portfile

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
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
// on, counted from 1.
type LintFix struct {
	Kind  string
	Lines []int
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
	for i, line := range lines {
		if !touched(i) {
			continue
		}
		if trimmed := strings.TrimRight(line, " \t"); trimmed != line && !escapedSpace(line, trimmed) {
			line = trimmed
			fix(FixTrailingSpace, i)
		}
		if expand {
			if indented := expandTabs(line); indented != line {
				line = indented
				fix(FixTabs, i)
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
			fixes = append(fixes, LintFix{Kind: kind, Lines: found[kind]})
		}
	}
	return []byte(strings.Join(lines, "\n")), fixes
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
	from := make([]int, len(after))
	for i := range from {
		from[i] = -1
	}
	// The common head and tail are matched outright, and the rest by the
	// subsequence's table, small for a change of a few lines.
	head := 0
	for head < len(before) && head < len(after) && before[head] == after[head] {
		from[head] = head
		head++
	}
	tail := 0
	for tail < len(before)-head && tail < len(after)-head && before[len(before)-1-tail] == after[len(after)-1-tail] {
		from[len(after)-1-tail] = len(before) - 1 - tail
		tail++
	}
	a, b := before[head:len(before)-tail], after[head:len(after)-tail]
	table := make([][]int32, len(a)+1)
	for i := range table {
		table[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else {
				table[i][j] = max(table[i+1][j], table[i][j+1])
			}
		}
	}
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i] == b[j]:
			from[head+j] = head + i
			i, j = i+1, j+1
		case table[i+1][j] >= table[i][j+1]:
			i++
		default:
			j++
		}
	}
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
