package portfile

import (
	"bytes"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// PortDependency is a port: dependency a Portfile writes: its line, the
// option that declares it, as depends_lib-append, and the port.
type PortDependency struct {
	Line   int
	Option string
	Port   string
}

// AddedPortDependencies are the port: dependencies after declares that
// before doesn't, by option and port, at the top or in any body, in the
// order written: the lines a change touched, which a reviewer's rule
// reads alone (the review survey's principle). A path:, lib:, or bin:
// dependency, or one written through a variable, isn't one; a version
// that doesn't parse adds none, and a before that doesn't parse is taken
// as declaring none.
func AddedPortDependencies(before, after []byte) []PortDependency {
	had := portDependencies(before)
	var added []PortDependency
	for _, dependency := range portDependencies(after) {
		if !slices.ContainsFunc(had, func(d PortDependency) bool { return d.Option == dependency.Option && d.Port == dependency.Port }) {
			added = append(added, dependency)
		}
	}
	return added
}

// portDependencies are every port: dependency src writes.
func portDependencies(src []byte) []PortDependency {
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
			if !literal || !strings.HasPrefix(spec, "port:") {
				continue
			}
			if dependency, err := macports.ParseDependency(phase, spec); err == nil {
				line := 1 + bytes.Count(src[:word.Span.Start], []byte("\n"))
				found = append(found, PortDependency{Line: line, Option: name, Port: dependency.Port})
			}
		}
	})
	return found
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
