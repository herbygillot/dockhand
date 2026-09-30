package portfile

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
	"path/filepath"
	"regexp"
	"strings"
)

// LocateDeclaration requires a unique source command inside the observed Tcl
// frame. Dynamic or ambiguous source locations are not editable.
func LocateDeclaration(src []byte, path string, declaration macports.Declaration) (syntax.Command, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return syntax.Command{}, fmt.Errorf("%w: invalid Tcl", ErrUnsupported)
	}
	var commands []syntax.Command
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		commands = append(commands, cmd)
	}

	foundFile := false
	for _, frame := range declaration.Frames {
		if filepath.Clean(frame.File) == filepath.Clean(path) {
			foundFile = true
		}
	}
	if !foundFile {
		if cmd, ok, err := locateInVariant(src, commands, declaration); ok {
			return cmd, err
		}
		return syntax.Command{}, fmt.Errorf("%w: declaration has no source location in the Portfile", ErrUnsupported)
	}
	var matches []syntax.Command
	for i := len(declaration.Frames) - 1; i >= 0; i-- {
		frame := declaration.Frames[i]
		for _, cmd := range commands {
			name, _ := cmd.Name(src)
			if name == strings.TrimPrefix(declaration.Command, "::") && sourceCommand(cmd.Span.Text(src)) == sourceCommand(frame.Command) {
				matches = append(matches, cmd)
			}
		}
		if len(matches) > 0 {
			if frame.File != "" && filepath.Clean(frame.File) != filepath.Clean(path) {
				return syntax.Command{}, fmt.Errorf("%w: declaration belongs to another source file", ErrUnsupported)
			}
			break
		}
	}
	if len(matches) > 1 {
		for i := len(declaration.Frames) - 1; i >= 0; i-- {
			frame := declaration.Frames[i]
			if filepath.Clean(frame.File) != filepath.Clean(path) {
				continue
			}
			for _, scope := range commands {
				scopeName, _ := scope.Name(src)
				if scopeName == strings.TrimPrefix(declaration.Command, "::") {
					continue
				}
				line, _ := text.Position(src, scope.Span.Start)
				if line != frame.Line || sourceCommand(scope.Span.Text(src)) != sourceCommand(frame.Command) {
					continue
				}
				var within []syntax.Command
				for _, cmd := range matches {
					if cmd.Span.Start >= scope.Span.Start && cmd.Span.End <= scope.Span.End {
						within = append(within, cmd)
					}
				}
				if len(within) == 1 {
					return within[0], nil
				}
			}
		}
	}
	switch len(matches) {
	case 0:
		return syntax.Command{}, fmt.Errorf("%w: no command written in the Portfile makes it, as an eval'd one isn't", ErrUnsupported)
	case 1:
		return matches[0], nil
	}
	return syntax.Command{}, fmt.Errorf("%w: declaration source is ambiguous (%d matches)", ErrUnsupported, len(matches))
}

// locateInVariant finds a declaration a variant's body makes. MacPorts
// runs the body as a procedure it builds from the body's text, so Tcl
// gives its commands no file: the stack names the procedure,
// variant-<name>, and then the command, by its text, as git's +doc makes
// its checksums-append. The command is the one of that text in the body of
// the Portfile's variant of that name, and nowhere else; one the body
// reaches through another procedure, or text the body has twice, isn't
// located. It reports false when the stack names no variant's procedure.
func locateInVariant(src []byte, commands []syntax.Command, declaration macports.Declaration) (syntax.Command, bool, error) {
	name := strings.TrimPrefix(declaration.Command, "::")
	for i, frame := range declaration.Frames {
		variant, ok := strings.CutPrefix(frame.Command, "variant-")
		if !ok || frame.File != "" || variant == "" || strings.ContainsAny(variant, " \t") {
			continue
		}
		if i+1 >= len(declaration.Frames) || firstWord(declaration.Frames[i+1].Command) != name {
			return syntax.Command{}, true, fmt.Errorf("%w: variant %s makes the declaration through another procedure", ErrUnsupported, variant)
		}
		inner := sourceCommand(declaration.Frames[i+1].Command)
		var bodies []text.Span
		for _, cmd := range commands {
			if command, _ := cmd.Name(src); command != "variant" || len(cmd.Words) < 3 {
				continue
			}
			if named, ok := cmd.Words[1].Literal(src); ok && named == variant {
				bodies = append(bodies, cmd.Words[len(cmd.Words)-1].Span)
			}
		}
		if len(bodies) != 1 {
			return syntax.Command{}, true, fmt.Errorf("%w: the Portfile defines variant %s %d times", ErrUnsupported, variant, len(bodies))
		}
		var matches []syntax.Command
		for _, cmd := range commands {
			command, _ := cmd.Name(src)
			if command == name && cmd.Span.Start >= bodies[0].Start && cmd.Span.End <= bodies[0].End && sourceCommand(cmd.Span.Text(src)) == inner {
				matches = append(matches, cmd)
			}
		}
		if len(matches) != 1 {
			return syntax.Command{}, true, fmt.Errorf("%w: variant %s's declaration source is ambiguous (%d matches)", ErrUnsupported, variant, len(matches))
		}
		return matches[0], true, nil
	}
	return syntax.Command{}, false, nil
}

func firstWord(command string) string {
	if fields := strings.Fields(command); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

var continuedLine = regexp.MustCompile(`\\\r?\n[ \t]*`)

func sourceCommand(value string) string {
	return strings.TrimSpace(continuedLine.ReplaceAllString(value, " "))
}

// RewriteLiteralDeclaration replaces the one declaration of command whose
// single literal argument is old. A value carried any other way is refused
// rather than guessed at.
func RewriteLiteralDeclaration(contents []byte, command, old, next string) ([]byte, error) {
	script, errs := syntax.Parse(contents)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%w: invalid Portfile syntax", ErrUnsupported)
	}
	var edits []text.Edit
	for cmd := range script.Commands(contents, func(syntax.Command) bool { return true }) {
		if name, _ := cmd.Name(contents); name != command || len(cmd.Words) != 2 {
			continue
		}
		if literal, ok := cmd.Words[1].Literal(contents); ok && literal == old && !cmd.Words[1].Expand {
			edits = append(edits, text.Edit{Span: cmd.Words[1].Span, New: []byte(next)})
		}
	}
	if len(edits) != 1 {
		return nil, fmt.Errorf("%w: %s is %s but no single literal declaration carries it", ErrUnsupported, command, old)
	}
	return text.Apply(contents, edits)
}
