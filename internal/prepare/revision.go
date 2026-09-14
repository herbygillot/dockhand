package prepare

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

func bumpRevision(src []byte, subport string, current int) ([]byte, error) {
	if current < 0 || current == int(^uint(0)>>1) {
		return nil, fmt.Errorf("%w: revision cannot be incremented", ErrUnsupported)
	}
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return nil, fmt.Errorf("%w: invalid Tcl syntax: %v", ErrUnsupported, errs)
	}
	if subport != "" {
		var matches []*syntax.Script
		for _, item := range script.Items {
			cmd, ok := item.(syntax.Command)
			if !ok {
				continue
			}
			name, ok := cmd.Name(src)
			if !ok || name != "subport" || len(cmd.Words) != 3 {
				continue
			}
			name, ok = cmd.Words[1].Literal(src)
			if !ok || name != subport {
				continue
			}
			if body, ok := cmd.Words[2].BracedScript(src); ok {
				matches = append(matches, body)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("%w: select a single literal subport block for %s", ErrUnsupported, subport)
		}
		script = matches[0]
	}
	var revisions []syntax.Command
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		if name, ok := cmd.Name(src); ok && name == "revision" {
			revisions = append(revisions, cmd)
		}
	}
	next := strconv.Itoa(current + 1)
	if len(revisions) > 1 {
		return nil, fmt.Errorf("%w: multiple revision commands in the selected scope", ErrUnsupported)
	}
	if len(revisions) == 1 {
		cmd := revisions[0]
		if len(cmd.Words) != 2 {
			return nil, fmt.Errorf("%w: revision is not a single literal argument", ErrUnsupported)
		}
		value, literal := cmd.Words[1].Literal(src)
		n, err := strconv.Atoi(value)
		if !literal || err != nil || n != current {
			return nil, fmt.Errorf("%w: revision expression does not have a matching literal", ErrUnsupported)
		}
		return text.Apply(src, []text.Edit{{Span: cmd.Words[1].Span, New: []byte(next)}})
	}
	if subport == "" && current != 0 {
		return nil, fmt.Errorf("%w: nonzero revision is set outside the supported scope", ErrUnsupported)
	}
	newline := "\n"
	if bytes.Contains(src, []byte("\r\n")) {
		newline = "\r\n"
	}
	insert := newline + "revision                " + next + newline
	if subport != "" {
		insert = newline + "    revision            " + next + newline
	}
	return text.Apply(src, []text.Edit{{Span: text.Span{Start: script.Span.End, End: script.Span.End}, New: []byte(insert)}})
}
