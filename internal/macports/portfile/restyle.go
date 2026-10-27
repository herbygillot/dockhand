package portfile

import (
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Restyle is a Portfile's text restyled whole, as `tidy --restyle` asks
// (Deferred's "Fixing a Portfile's alignment automatically", reopened as
// opt-in, 2026-10-27): trailing whitespace goes; leading tabs become
// spaces under MacPorts' modeline; each command is indented four spaces a
// block, its continuation lines moving with it; and each run of
// declarations, one to a line with nothing between, has its values at the
// column most of the run already uses, each one's continuation lines that
// were lined up with its value moving with it. A command with a word of
// several lines that isn't a body, as a braced long_description, keeps
// its lines, whose spaces may be the word's. changed is false where the
// text was left as it was. Nothing it does changes what a port evaluates
// to, which the caller proves by evaluating both.
func Restyle(src []byte) (_ []byte, changed bool) {
	lines := splitLines(src)
	expand := len(lines) > 0 && isModeline(lines[0]) && !conflicts(lines[0])
	for i, line := range lines {
		if trimmed := strings.TrimRight(line, " \t"); !escapedSpace(line, trimmed) {
			line = trimmed
		}
		if expand {
			line = expandTabs(line)
		}
		lines[i] = line
	}
	text := []byte(strings.Join(lines, "\n"))
	text = indentInFours(text)
	text = alignRuns(text)
	return text, string(text) != string(src)
}

// layout is a text's lines, where each starts, and which line a position
// is on.
type layout struct {
	src    []byte
	starts []int
}

func newLayout(src []byte) layout {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return layout{src: src, starts: starts}
}

// line is the line, counted from 0, a position is on.
func (l layout) line(at int) int {
	i, found := slices.BinarySearch(l.starts, at)
	if !found {
		i--
	}
	return i
}

// column is a position's column on its line.
func (l layout) column(at int) int { return at - l.starts[l.line(at)] }

// startsLine reports whether a position starts its line's text, nothing
// but spaces before it.
func (l layout) startsLine(at int) bool {
	return strings.TrimLeft(string(l.src[l.starts[l.line(at)]:at]), " ") == ""
}

// indent is the spaces a line starts with.
func (l layout) indent(line int) int {
	end := len(l.src)
	if line+1 < len(l.starts) {
		end = l.starts[line+1] - 1
	}
	text := string(l.src[l.starts[line]:end])
	return len(text) - len(strings.TrimLeft(text, " "))
}

// reindented is a text with the given lines' indents set, each line
// counted from 0; a blank line keeps none.
func reindented(src []byte, indents map[int]int) []byte {
	lines := splitLines(src)
	for i, indent := range indents {
		body := strings.TrimLeft(lines[i], " ")
		if body == "" || indent < 0 {
			continue
		}
		lines[i] = strings.Repeat(" ", indent) + body
	}
	return []byte(strings.Join(lines, "\n"))
}

// bodyWords are the words of a command MacPorts runs as scripts: a
// control structure's bodies, and the last word of a procedure, a
// platform, variant, or subport block, or a phase's.
func bodyWords(src []byte, cmd syntax.Command) []syntax.Word {
	if _, words, ok := cmd.Control(src); ok {
		return words
	}
	name, _ := cmd.Name(src)
	switch {
	case len(cmd.Words) < 2:
	case name == "proc" || name == "platform" || name == "variant" || name == "subport" || strings.HasPrefix(name, "pre-") || strings.HasPrefix(name, "post-") ||
		slices.Contains([]string{"fetch", "checksum", "extract", "patch", "configure", "build", "test", "destroot", "install", "activate"}, name) && len(cmd.Words) == 2:
		return cmd.Words[len(cmd.Words)-1:]
	}
	return nil
}

// tclCommands are the commands a declarations' block holds that are no
// option's declaration, as set or file, whose words aren't lined up as
// values; PortSystem and PortGroup are written with one space.
var tclCommands = []string{"set", "global", "variable", "upvar", "puts", "file", "ln", "xinstall", "system", "reinplace", "copy", "move", "delete", "touch",
	"lappend", "append", "incr", "unset", "return", "exec", "source", "foreach", "ui_msg", "ui_warn", "ui_debug", "ui_error", "ui_info", "known_fail",
	"PortSystem", "PortGroup", "notes"}

// spanned reports whether a command has a word of several lines that
// isn't a body, whose spaces may be the word's own.
func spanned(l layout, cmd syntax.Command, bodies []syntax.Word) bool {
	for _, word := range cmd.Words {
		if l.line(word.Span.Start) != l.line(word.Span.End-1) && !slices.ContainsFunc(bodies, func(body syntax.Word) bool { return body.Span == word.Span }) {
			return true
		}
	}
	return false
}

// indentInFours indents each command four spaces a block, its
// continuation lines and a body's closing brace moving with it.
func indentInFours(src []byte) []byte {
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return src
	}
	l := newLayout(src)
	indents := map[int]int{}
	var walk func(script *syntax.Script, depth int)
	walk = func(script *syntax.Script, depth int) {
		for _, item := range script.Items {
			switch item := item.(type) {
			case syntax.Comment:
				if l.startsLine(item.Span.Start) && l.line(item.Span.Start) == l.line(item.Span.End-1) {
					indents[l.line(item.Span.Start)] = 4 * depth
				}
			case syntax.Command:
				bodies := bodyWords(src, item)
				var scripts []*syntax.Script
				for _, body := range bodies {
					if inner, ok := body.BracedScript(src); ok {
						scripts = append(scripts, inner)
					}
				}
				if l.startsLine(item.Span.Start) && !spanned(l, item, bodies) && len(scripts) == len(bodies) {
					first := l.line(item.Span.Start)
					delta := 4*depth - l.indent(first)
					indents[first] = 4 * depth
					inside := func(line int) bool {
						return slices.ContainsFunc(bodies, func(body syntax.Word) bool {
							return line > l.line(body.Span.Start) && line <= l.line(body.Span.End-1)
						})
					}
					for line := first + 1; line <= l.line(item.Span.End-1); line++ {
						if !inside(line) {
							indents[line] = max(l.indent(line)+delta, 0)
						}
					}
					for _, body := range bodies {
						if end := l.line(body.Span.End - 1); end != l.line(body.Span.Start) && l.startsLine(body.Span.End-1) {
							indents[end] = 4 * depth
						}
					}
				}
				for _, inner := range scripts {
					walk(inner, depth+1)
				}
			}
		}
	}
	walk(script, 0)
	return reindented(src, indents)
}

// alignRuns lines up each run of declarations' values at the column most
// of the run uses, the one the Portfile's declarations use most where as
// many of the run use each, or past the name where it's longer: a run is options' declarations one to a line,
// each a name and a value on that line with no tab before the value, with
// no blank line, comment, or other command between them, at the top or in
// a subport's, variant's, platform's, or if's block.
func alignRuns(src []byte) []byte {
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return src
	}
	l := newLayout(src)
	lines := splitLines(src)
	type member struct {
		cmd    syntax.Command
		line   int
		column int
	}
	// whole counts the columns every run uses, which a run that uses two
	// as often takes the Portfile's from.
	whole := map[int]int{}
	var runs [][]member
	align := func(run []member) {
		if len(run) < 2 {
			return
		}
		counts := map[int]int{}
		for _, m := range run {
			counts[m.column]++
		}
		column := -1
		for c, n := range counts {
			if column < 0 || n > counts[column] || n == counts[column] && (whole[c] > whole[column] || whole[c] == whole[column] && c < column) {
				column = c
			}
		}
		for _, m := range run {
			name := m.cmd.Words[0].Span
			want := max(column, l.column(name.End)+1)
			delta := want - m.column
			if delta == 0 {
				continue
			}
			line := lines[m.line]
			end, value := l.column(name.End), m.column
			lines[m.line] = line[:end] + strings.Repeat(" ", want-end) + line[value:]
			for next := m.line + 1; next <= l.line(m.cmd.Span.End-1); next++ {
				if l.indent(next) == m.column && strings.TrimSpace(lines[next]) != "" {
					lines[next] = strings.Repeat(" ", want) + strings.TrimLeft(lines[next], " ")
				}
			}
		}
	}
	var walk func(script *syntax.Script)
	walk = func(script *syntax.Script) {
		var run []member
		last := -2
		for _, item := range script.Items {
			cmd, ok := item.(syntax.Command)
			bodies := []syntax.Word(nil)
			if ok {
				bodies = bodyWords(src, cmd)
			}
			declaration := ok && len(cmd.Words) >= 2 && len(bodies) == 0 && !spanned(l, cmd, nil)
			if declaration {
				name, literal := cmd.Words[0].Literal(src)
				declaration = literal && !slices.Contains(tclCommands, name) && l.startsLine(cmd.Span.Start) && l.line(cmd.Words[1].Span.Start) == l.line(cmd.Span.Start) &&
					!strings.Contains(string(src[l.starts[l.line(cmd.Span.Start)]:cmd.Words[1].Span.Start]), "\t")
			}
			if !declaration || l.line(cmd.Span.Start) != last+1 {
				runs = append(runs, run)
				run = nil
			}
			if declaration {
				run = append(run, member{cmd: cmd, line: l.line(cmd.Span.Start), column: l.column(cmd.Words[1].Span.Start)})
				last = l.line(cmd.Span.End - 1)
			} else {
				last = -2
			}
			// A block of declarations is read too, a subport's, a
			// variant's, a platform's, or an if's; a phase's or a
			// procedure's is code.
			if name, _ := cmd.Name(src); ok && slices.Contains([]string{"subport", "variant", "platform", "if"}, name) {
				for _, body := range bodies {
					if inner, ok := body.BracedScript(src); ok {
						walk(inner)
					}
				}
			}
		}
		runs = append(runs, run)
	}
	walk(script)
	for _, run := range runs {
		for _, m := range run {
			whole[m.column]++
		}
	}
	for _, run := range runs {
		align(run)
	}
	return []byte(strings.Join(lines, "\n"))
}
