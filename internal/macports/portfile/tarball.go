package portfile

import (
	"bytes"
	"regexp"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// ArchiveFromTarball is a Portfile's text with `github.tarball_from
// tarball` made `github.tarball_from archive`: GitHub's archive of the
// tag, which MacPorts prefers to its tarball, and which the github
// PortGroup extracts to the same directory (the review survey's §4). A
// version update makes it, since its checksums are made anew anyway. It
// moves only the one command that sets the option, at the Portfile's top
// or in the subport's own block, as a single literal, and the PortGroup's
// stock comment above it that asks for the move; false where there's
// none, or another sets it, as in an if, which is the maintainer's to
// move.
func ArchiveFromTarball(src []byte, subport string) ([]byte, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return src, false
	}
	var all []syntax.Command
	commands(src, script, false, func(cmd syntax.Command, _ bool) {
		if name, _ := cmd.Name(src); name == "github.tarball_from" {
			all = append(all, cmd)
		}
	})
	if len(all) != 1 || len(all[0].Words) != 2 {
		return src, false
	}
	cmd := all[0]
	if value, ok := cmd.Words[1].Literal(src); !ok || value != "tarball" {
		return src, false
	}
	scope := script.Items
	if subport != "" {
		if body, ok := subportBody(src, script, subport); ok {
			scope = append(scope, body.Items...)
		}
	}
	for _, item := range scope {
		if found, ok := item.(syntax.Command); ok && found.Span == cmd.Span {
			edits := []textedit.Edit{{Span: cmd.Words[1].Span, New: []byte("archive")}}
			// The github PortGroup's stock comment, which asks for this
			// move, goes with it.
			start := bytes.LastIndexByte(src[:cmd.Span.Start], '\n') + 1
			if start > 0 {
				above := bytes.LastIndexByte(src[:start-1], '\n') + 1
				if stockComment.Match(src[above : start-1]) {
					edits = append([]textedit.Edit{{Span: textedit.Span{Start: above, End: start}}}, edits...)
				}
			}
			moved, err := textedit.Apply(src, edits)
			return moved, err == nil
		}
	}
	return src, false
}

// stockComment is the line the github PortGroup's ports carry above
// github.tarball_from tarball, asking for the move at the next update.
var stockComment = regexp.MustCompile(`^\s*# Change github\.tarball_from to 'releases' or 'archive' (?:next update|with next version update)\s*$`)
