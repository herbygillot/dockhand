package portfile

import (
	"bytes"
	"cmp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// PortDependency is a port: dependency a Portfile writes: its line, the
// option that declares it, as depends_lib-append, the port, and the
// byte span of its word.
type PortDependency struct {
	Line       int
	Option     string
	Port       string
	Start, End int
}

// AddedPortDependencies are the port: dependencies after declares that
// before doesn't, by option and port, at the top or in any body, in the
// order written: the lines a change touched, which a reviewer's rule
// reads alone (the review survey's principle). A path:, lib:, or bin:
// dependency, or one written through a variable, isn't one; a version
// that doesn't parse adds none, and a before that doesn't parse is taken
// as declaring none.
func AddedPortDependencies(before, after []byte) []PortDependency {
	had := PortDependencies(before)
	var added []PortDependency
	for _, dependency := range PortDependencies(after) {
		if !slices.ContainsFunc(had, func(d PortDependency) bool { return d.Option == dependency.Option && d.Port == dependency.Port }) {
			added = append(added, dependency)
		}
	}
	return added
}

// PortDependencies are every literal port: dependency src writes, at the
// top or in any body, in the order written; none where it doesn't parse.
func PortDependencies(src []byte) []PortDependency {
	return dependencies(src, true)
}

// WrittenDependencies are every literal dependency src writes, port:,
// path:, lib:, and bin: alike, each by the port it names, at the top or in
// any body, in the order written; none where it doesn't parse: what the
// Portfile's own text declares, which a PortGroup's additions aren't.
func WrittenDependencies(src []byte) []PortDependency {
	return dependencies(src, false)
}

// dependencies are src's literal dependencies, its port: ones alone where
// portOnly.
func dependencies(src []byte, portOnly bool) []PortDependency {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil
	}
	var found []PortDependency
	commands(src, script, false, func(cmd syntax.Command, _ bool) {
		name, _ := cmd.Name(src)
		option, _, _ := strings.Cut(name, "-")
		phase, ok := strings.CutPrefix(option, "depends_")
		if !ok {
			return
		}
		for _, word := range cmd.Words[1:] {
			spec, literal := word.Literal(src)
			if !literal || portOnly && !strings.HasPrefix(spec, "port:") {
				continue
			}
			if dependency, err := macports.ParseDependency(phase, spec); err == nil {
				line := 1 + bytes.Count(src[:word.Span.Start], []byte("\n"))
				found = append(found, PortDependency{Line: line, Option: name, Port: dependency.Port, Start: word.Span.Start, End: word.Span.End})
			}
		}
	})
	return found
}

// RewriteDependency writes spec in place of a port: dependency's word,
// nothing else on its line moved, keeping any quoting around it; src as
// it is where the word there isn't port:<its port>.
func RewriteDependency(src []byte, dependency PortDependency, spec string) []byte {
	if dependency.Start < 0 || dependency.End > len(src) || dependency.Start >= dependency.End {
		return src
	}
	word := src[dependency.Start:dependency.End]
	at := bytes.Index(word, []byte("port:"+dependency.Port))
	if at < 0 {
		return src
	}
	start := dependency.Start + at
	return slices.Concat(src[:start], []byte(spec), src[start+len("port:"+dependency.Port):])
}

// AddedLine is a command a change adds at a Portfile's top: its line, the
// option it sets, its own text, and the Portfile without it, the
// alternative a rule evaluates to test whether the line changes anything.
type AddedLine struct {
	Line    int
	Option  string
	Text    string
	Without []byte
}

// AddedLines are the commands at after's top, one to a line, whose text
// before has nowhere at its top, in the order written: the lines a change
// touched (the review survey's principle). A command spanning several
// lines, or in a body, isn't one; a version that doesn't parse adds none,
// and a before that doesn't parse is taken as having none.
func AddedLines(before, after []byte) []AddedLine {
	had := map[string]bool{}
	if script, errs := syntax.Parse(before); len(errs) == 0 {
		for _, item := range script.Items {
			if cmd, ok := item.(syntax.Command); ok {
				had[strings.Join(strings.Fields(string(before[cmd.Span.Start:cmd.Span.End])), " ")] = true
			}
		}
	}
	script, errs := syntax.Parse(after)
	if len(errs) > 0 {
		return nil
	}
	var added []AddedLine
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok || len(cmd.Words) < 2 {
			continue
		}
		text := string(after[cmd.Span.Start:cmd.Span.End])
		normal := strings.Join(strings.Fields(text), " ")
		name, literal := cmd.Name(after)
		if !literal || had[normal] || strings.Contains(strings.TrimRight(text, "\n"), "\n") {
			continue
		}
		start := bytes.LastIndexByte(after[:cmd.Span.Start], '\n') + 1
		end := cmd.Span.End
		if next := bytes.IndexByte(after[end:], '\n'); next >= 0 {
			end += next + 1
		} else {
			end = len(after)
		}
		without := slices.Concat(after[:start], after[end:])
		added = append(added, AddedLine{Line: 1 + bytes.Count(after[:cmd.Span.Start], []byte("\n")), Option: name, Text: normal, Without: without})
	}
	return added
}

// TopWords are the literal words of option, and of option-append, at
// src's top, in the order written: what the Portfile sets it to by its
// default variants, as license_noconflict; none where it doesn't parse,
// and none of a word written through a variable.
func TopWords(src []byte, option string) []string {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil
	}
	var words []string
	commands(src, script, false, func(cmd syntax.Command, nested bool) {
		name, _ := cmd.Name(src)
		if nested || name != option && name != option+"-append" {
			return
		}
		for _, word := range cmd.Words[1:] {
			if value, literal := word.Literal(src); literal {
				words = append(words, value)
			}
		}
	})
	return words
}

// DeclareLibrary writes "depends_lib-append  <spec>" at a Portfile's top:
// after its last depends_lib command, else its last depends_ command,
// else its last checksums command, aligned as that line's value is, as
// tidy declares what a check found the build linking and not declaring.
// False where the Portfile doesn't parse, has none of those at its top,
// or the line holds another command.
func DeclareLibrary(src []byte, spec string) ([]byte, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, false
	}
	var lib, any, sums *syntax.Command
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		name, _ := cmd.Name(src)
		switch {
		case strings.HasPrefix(name, "depends_lib"):
			lib, any = &cmd, &cmd
		case strings.HasPrefix(name, "depends_"):
			any = &cmd
		case strings.HasPrefix(name, "checksums"):
			sums = &cmd
		}
	}
	anchor := cmp.Or(lib, any, sums)
	if anchor == nil || strings.TrimSpace(string(src[lineStart(src, anchor.Span.Start):anchor.Span.Start])) != "" {
		return nil, false
	}
	end := lineEnd(src, anchor.Span.End)
	if strings.TrimSpace(string(src[anchor.Span.End:end])) != "" {
		return nil, false
	}
	const option = "depends_lib-append"
	column := max(valueColumn(src, *anchor), len(option)+1)
	line := "\n" + option + strings.Repeat(" ", column-len(option)) + spec
	if anchor == sums {
		line = "\n" + line
	}
	return slices.Concat(src[:end], []byte(line), src[end:]), true
}

// DeclaredChanges are the ports after declares at its top that before
// doesn't, ok where that is the whole difference: every other line the
// same, but for blank lines, the dependencies before declares all still
// declared, each added one literal, port:, path:, lib:, or bin:, by the
// port it names. It's how a check
// after tidy declares a library its builds linked reuses what they built
// (docs/designs/declare-linked-libraries.md, part 3). False where either
// doesn't parse, or anything else differs.
func DeclaredChanges(before, after []byte) (ports []string, ok bool) {
	strippedBefore, okBefore := withoutTopDependencies(before)
	strippedAfter, okAfter := withoutTopDependencies(after)
	if !okBefore || !okAfter || strippedBefore != strippedAfter {
		return nil, false
	}
	had, has := WrittenDependencies(before), WrittenDependencies(after)
	same := func(a, b PortDependency) bool { return a.Option == b.Option && a.Port == b.Port }
	for _, dependency := range had {
		if !slices.ContainsFunc(has, func(d PortDependency) bool { return same(d, dependency) }) {
			return nil, false
		}
	}
	for _, dependency := range has {
		if !slices.ContainsFunc(had, func(d PortDependency) bool { return same(d, dependency) }) && !slices.Contains(ports, dependency.Port) {
			ports = append(ports, dependency.Port)
		}
	}
	return ports, len(ports) > 0
}

// withoutTopDependencies is a Portfile's text without the lines of its
// top-level depends_ commands, and its blank lines; false where it doesn't
// parse, or such a command shares a line with another.
func withoutTopDependencies(src []byte) (string, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return "", false
	}
	var spans [][2]int
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		if name, _ := cmd.Name(src); !strings.HasPrefix(name, "depends_") {
			continue
		}
		start, end := lineStart(src, cmd.Span.Start), lineEnd(src, cmd.Span.End)
		if strings.TrimSpace(string(src[start:cmd.Span.Start])) != "" || strings.TrimSpace(string(src[cmd.Span.End:end])) != "" {
			return "", false
		}
		spans = append(spans, [2]int{start, end})
	}
	var kept []byte
	at := 0
	for _, span := range spans {
		kept = append(kept, src[at:span[0]]...)
		at = span[1]
	}
	kept = append(kept, src[at:]...)
	var lines []string
	for _, line := range strings.Split(string(kept), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimRight(line, " \t"))
		}
	}
	return strings.Join(lines, "\n"), true
}
