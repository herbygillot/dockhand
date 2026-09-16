package portfile

import (
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
	"path/filepath"
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
		return syntax.Command{}, fmt.Errorf("%w: declaration has no source location in the Portfile", ErrUnsupported)
	}
	var matches []syntax.Command
	for i := len(declaration.Frames) - 1; i >= 0; i-- {
		frame := declaration.Frames[i]
		for _, cmd := range commands {
			name, _ := cmd.Name(src)
			if name == strings.TrimPrefix(declaration.Command, "::") && strings.TrimSpace(cmd.Span.Text(src)) == strings.TrimSpace(frame.Command) {
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
				line, _ := text.Position(src, scope.Span.Start)
				if line != frame.Line || strings.TrimSpace(scope.Span.Text(src)) != strings.TrimSpace(frame.Command) {
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
	if len(matches) != 1 {
		return syntax.Command{}, fmt.Errorf("%w: declaration source is ambiguous (%d matches)", ErrUnsupported, len(matches))
	}
	return matches[0], nil
}
