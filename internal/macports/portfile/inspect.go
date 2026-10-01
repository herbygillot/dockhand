package portfile

import (
	"bytes"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// A conservative inspection of a Portfile's source: what it can prove from
// the source as Tcl reads it, and nothing it can't. A command inside a
// word that isn't a script, such as the value of a set, is data, never a
// declaration (the private-helper review's finding 2).

// bodies are the words of a command MacPorts runs as scripts: a control
// structure's bodies, as the parser knows them, and the last word of a
// procedure or a platform, variant, or subport block.
func bodies(src []byte, cmd syntax.Command) []*syntax.Script {
	var scripts []*syntax.Script
	if _, words, ok := cmd.Control(src); ok {
		for _, word := range words {
			if script, ok := word.BracedScript(src); ok {
				scripts = append(scripts, script)
			}
		}
		return scripts
	}
	switch name, _ := cmd.Name(src); name {
	case "proc", "platform", "variant", "subport":
		if script, ok := cmd.Words[len(cmd.Words)-1].BracedScript(src); ok {
			scripts = append(scripts, script)
		}
	}
	return scripts
}

// commands are a script's commands, then each body's, as MacPorts would
// run them, each with whether it's inside a body.
func commands(src []byte, script *syntax.Script, nested bool, visit func(cmd syntax.Command, nested bool)) {
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		visit(cmd, nested)
		for _, body := range bodies(src, cmd) {
			commands(src, body, true, visit)
		}
	}
}

// RunCommand is a command MacPorts runs, and whether it's in a body, which
// may or may not run, rather than at the Portfile's top, which always does.
type RunCommand struct {
	syntax.Command
	Nested bool
}

// RunCommands are the commands of a Portfile MacPorts runs, at its top and
// in each body it runs, a control structure's, a procedure's, or a
// platform, variant, or subport block's, in the order they're written; a
// command inside data isn't one. False where the source doesn't parse.
func RunCommands(src []byte) ([]RunCommand, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, false
	}
	var found []RunCommand
	commands(src, script, false, func(cmd syntax.Command, nested bool) { found = append(found, RunCommand{Command: cmd, Nested: nested}) })
	return found, true
}

// RevisionOnly reports whether two versions of a Portfile differ only in
// their revision declarations, the revision commands MacPorts runs: each
// set aside, with its line where it stands alone on one, the rest of the
// source must be the same, byte for byte, so a revision added where there
// was none is one too. A "revision 2" inside data, such as a set's value,
// is the source's, and its change is a change. It reports false where it
// can't parse either.
func RevisionOnly(before, after []byte) bool {
	a, okA := withoutRevisions(before)
	b, okB := withoutRevisions(after)
	return okA && okB && bytes.Equal(a, b)
}

func withoutRevisions(src []byte) ([]byte, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, false
	}
	var edits []text.Edit
	commands(src, script, false, func(cmd syntax.Command, _ bool) {
		if name, _ := cmd.Name(src); name != "revision" || len(cmd.Words) != 2 {
			return
		}
		span := cmd.Span
		start := bytes.LastIndexByte(src[:span.Start], '\n') + 1
		end := span.End + bytes.IndexByte(append(src[span.End:], '\n'), '\n')
		if len(bytes.TrimSpace(src[start:span.Start])) == 0 && len(bytes.TrimSpace(src[span.End:end])) == 0 {
			span = text.Span{Start: start, End: min(end+1, len(src))}
		}
		edits = append(edits, text.Edit{Span: span})
	})
	out, err := text.Apply(src, edits)
	return out, err == nil
}

// DeclaredVersion is the version a Portfile declares for a port, where it
// declares it literally and unconditionally: a version command, or a
// PortGroup's setup command that carries one, the last of them, as the
// last one MacPorts runs wins. A subport's own declaration in its literal
// subport block comes first, and otherwise it has the main port's; another
// subport's is never the port's, as git-devel's github.setup isn't git's.
// It reports false where the version is computed, conditional, or can't
// be told.
func DeclaredVersion(src []byte, subport string) (string, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return "", false
	}
	if subport != "" {
		body, ok := subportBody(src, script, subport)
		if !ok {
			return "", false
		}
		version, declared, ok := versionIn(src, body)
		switch {
		case !ok:
			return "", false
		case declared:
			return version, true
		}
	}
	version, declared, ok := versionIn(src, script)
	if !ok || !declared {
		return "", false
	}
	return version, true
}

// versionIn reads a scope's version declaration: declared is false where
// it makes none, and ok is false where one it makes can't be proven.
func versionIn(src []byte, script *syntax.Script) (version string, declared, ok bool) {
	ok = true
	for _, item := range script.Items {
		cmd, isCommand := item.(syntax.Command)
		if !isCommand {
			continue
		}
		if index := versionIndex(src, cmd); index > 0 {
			value, literal := cmd.Words[index].Literal(src)
			if !literal || !Literal(value) || cmd.Words[index].Expand {
				return "", true, false
			}
			version, declared = value, true
		}
		// A version declared inside a body, a condition's or a variant's,
		// may or may not be the one MacPorts runs; a subport's is another
		// port's.
		if name, _ := cmd.Name(src); name == "subport" || name == "proc" {
			continue
		}
		for _, body := range bodies(src, cmd) {
			commands(src, body, true, func(inner syntax.Command, _ bool) {
				if versionIndex(src, inner) > 0 {
					ok = false
				}
			})
		}
	}
	return version, declared, ok
}

// versionIndex is the word of a command that sets the port's version: a
// version command's value, or a setup command's version; 0 for any other.
func versionIndex(src []byte, cmd syntax.Command) int {
	name, _ := cmd.Name(src)
	if name == "version" && len(cmd.Words) == 2 {
		return 1
	}
	return max(setupVersionIndex(name, len(cmd.Words)), 0)
}

// subportBody is the one block of a subport with a literal name.
func subportBody(src []byte, script *syntax.Script, subport string) (*syntax.Script, bool) {
	var found []*syntax.Script
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		if name, _ := cmd.Name(src); name != "subport" || len(cmd.Words) != 3 {
			continue
		}
		if name, ok := cmd.Words[1].Literal(src); ok && name == subport {
			if body, ok := cmd.Words[2].BracedScript(src); ok {
				found = append(found, body)
			}
		}
	}
	if len(found) != 1 {
		return nil, false
	}
	return found[0], true
}

// DeclaredRevision is the revision a Portfile declares for its main port,
// literally and unconditionally, and the line it's on; false where it
// declares none it can prove.
func DeclaredRevision(src []byte) (value string, line int, ok bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return "", 0, false
	}
	var found []syntax.Command
	for _, item := range script.Items {
		if cmd, isCommand := item.(syntax.Command); isCommand {
			if name, _ := cmd.Name(src); name == "revision" && len(cmd.Words) == 2 {
				found = append(found, cmd)
			}
		}
	}
	if len(found) == 0 {
		return "", 0, false
	}
	last := found[len(found)-1]
	value, ok = last.Words[1].Literal(src)
	if !ok || !Literal(value) {
		return "", 0, false
	}
	line, _ = text.Position(src, last.Words[1].Span.Start)
	return value, line, true
}

// PortGroupReferences are the PortGroups a Portfile, or a PortGroup,
// loads, each named literally, wherever MacPorts would run its PortGroup
// command. conclusive is false where the source could load another it
// doesn't name: a PortGroup command whose name or version isn't literal,
// or a mention of the ports tree's _resources outside a comment, as a
// source command reaching into it would be.
func PortGroupReferences(src []byte) (references []macports.PortGroup, conclusive bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, false
	}
	conclusive = true
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") && strings.Contains(line, macports.ResourcesDirectory) {
			conclusive = false
		}
	}
	commands(src, script, false, func(cmd syntax.Command, _ bool) {
		if name, _ := cmd.Name(src); name != "PortGroup" {
			return
		}
		if len(cmd.Words) != 3 {
			conclusive = false
			return
		}
		group, okName := cmd.Words[1].Literal(src)
		version, okVersion := cmd.Words[2].Literal(src)
		if !okName || !okVersion || !Literal(group) || !Literal(version) {
			conclusive = false
			return
		}
		references = append(references, macports.PortGroup{Name: group, Version: version})
	})
	return references, conclusive
}
