package portfile

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// stealthSubdir is the dist_subdir the MacPorts guide gives a stealth
// update, numbered for each one: ${name}/${version}_1.
var stealthSubdir = regexp.MustCompile(`^\$\{name\}/\$\{version\}_([0-9]+)$`)

// StealthDistSubdir keeps a distfile that changed upstream without a new
// name apart from the old one, so mirrors keep both: it sets dist_subdir
// ${name}/${version}_1 after the checksums, or counts up one already set
// that way. It returns the number it wrote. A dist_subdir set any other
// way, or inside a block, is the person's to change.
func StealthDistSubdir(src []byte) ([]byte, int, error) {
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return nil, 0, fmt.Errorf("%w: invalid Tcl syntax: %v", ErrUnsupported, errs)
	}
	var subdirs, checksums []syntax.Command
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		switch name, _ := cmd.Name(src); name {
		case "dist_subdir":
			subdirs = append(subdirs, cmd)
		case "checksums":
			checksums = append(checksums, cmd)
		}
	}
	switch {
	case len(subdirs) > 1:
		return nil, 0, fmt.Errorf("%w: dist_subdir is set more than once", ErrUnsupported)
	case len(subdirs) == 1:
		cmd := subdirs[0]
		if len(cmd.Words) != 2 {
			return nil, 0, fmt.Errorf("%w: dist_subdir is not one word", ErrUnsupported)
		}
		value := string(src[cmd.Words[1].Span.Start:cmd.Words[1].Span.End])
		m := stealthSubdir.FindStringSubmatch(value)
		if m == nil {
			return nil, 0, fmt.Errorf("%w: dist_subdir is already set, to %s", ErrUnsupported, value)
		}
		n, _ := strconv.Atoi(m[1])
		next := fmt.Sprintf("${name}/${version}_%d", n+1)
		out, err := text.Apply(src, []text.Edit{{Span: cmd.Words[1].Span, New: []byte(next)}})
		return out, n + 1, err
	}
	newline := "\n"
	if bytes.Contains(src, []byte("\r\n")) {
		newline = "\r\n"
	}
	at, width := script.Span.End, 24
	if len(checksums) > 0 {
		last := checksums[len(checksums)-1]
		at = last.Span.End
		if len(last.Words) > 1 {
			lineStart := bytes.LastIndexByte(src[:last.Span.Start], '\n') + 1
			width = last.Words[1].Span.Start - lineStart
		}
	}
	width = max(width, len("dist_subdir")+1)
	line := "dist_subdir" + strings.Repeat(" ", width-len("dist_subdir")) + "${name}/${version}_1"
	insert := newline + line
	if at == script.Span.End && at > 0 && src[at-1] == '\n' {
		insert = line + newline
	}
	out, err := text.Apply(src, []text.Edit{{Span: text.Span{Start: at, End: at}, New: []byte(insert)}})
	return out, 1, err
}
