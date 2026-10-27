package portfile

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// RaiseEpoch raises the target's epoch from current by one, as an update to
// a version MacPorts orders lower needs for `port upgrade` to take it (the
// docs dig's I3): at the literal of the one epoch command in the target's
// scope, the subport's block or the Portfile's top level; where there is
// none, a new line after the revision, else after the version, aligned with
// it. An epoch command anywhere else, as py-openssl's inside an if, is the
// Portfile's own reckoning, whatever it evaluates to, so it is refused
// rather than written over.
func RaiseEpoch(src []byte, subport string, current int) ([]byte, error) {
	if current < 0 || current == int(^uint(0)>>1) {
		return nil, ErrUnsupported.Because("epoch cannot be incremented")
	}
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return nil, ErrUnsupported.Because("invalid Tcl syntax: %v", errs)
	}
	scope := script
	if subport != "" {
		if body, ok := subportBody(src, script, subport); ok {
			scope = body
		} else {
			subport = ""
		}
	}
	var epochs []syntax.Command
	for _, item := range scope.Items {
		if cmd, ok := item.(syntax.Command); ok {
			if name, ok := cmd.Name(src); ok && name == "epoch" {
				epochs = append(epochs, cmd)
			}
		}
	}
	if len(epochCommand.FindAll(src, -1)) != len(epochs) {
		return nil, ErrUnsupported.Because("epoch is set outside the selected scope, as inside a condition")
	}
	next := strconv.Itoa(current + 1)
	switch len(epochs) {
	case 0:
	case 1:
		cmd := epochs[0]
		if len(cmd.Words) != 2 {
			return nil, ErrUnsupported.Because("epoch is not a single literal argument")
		}
		value, literal := cmd.Words[1].Literal(src)
		if n, err := strconv.Atoi(value); !literal || err != nil || n != current {
			return nil, ErrUnsupported.Because("epoch expression does not have a matching literal")
		}
		return textedit.Apply(src, []textedit.Edit{{Span: cmd.Words[1].Span, New: []byte(next)}})
	default:
		return nil, ErrUnsupported.Because("multiple epoch commands in the selected scope")
	}
	if current != 0 {
		return nil, ErrUnsupported.Because("nonzero epoch is set outside the supported scope")
	}
	newline := "\n"
	if bytes.Contains(src, []byte("\r\n")) {
		newline = "\r\n"
	}
	// After the revision, else the version, as MacPorts writes them,
	// aligned with it; failing that, at the scope's end.
	for _, name := range []string{"revision", "version"} {
		if after, ok := lastCommand(src, scope, name); ok && len(after.Words) > 1 {
			lineStart := bytes.LastIndexByte(src[:after.Span.Start], '\n') + 1
			indent := src[lineStart:after.Span.Start]
			width := max(after.Words[1].Span.Start-after.Span.Start, len("epoch")+1)
			line := string(indent) + "epoch" + strings.Repeat(" ", width-len("epoch")) + next
			return textedit.Apply(src, []textedit.Edit{{Span: textedit.Span{Start: after.Span.End, End: after.Span.End}, New: []byte(newline + line)}})
		}
	}
	insert := newline + "epoch                   " + next + newline
	if subport != "" {
		insert = newline + "    epoch               " + next + newline
	}
	return textedit.Apply(src, []textedit.Edit{{Span: textedit.Span{Start: scope.Span.End, End: scope.Span.End}, New: []byte(insert)}})
}

// epochCommand is an epoch command wherever it is: at a line's start, or
// after a brace or semicolon, as in a one-line if.
var epochCommand = regexp.MustCompile(`(?m)(?:^|[{;])[ \t]*epoch[ \t]`)
