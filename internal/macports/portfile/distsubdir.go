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

// The dist_subdir forms a stealth update writes: numbered for each one,
// ${name}/${version}_1 as the MacPorts guide gives it, or following the
// revision the update bumps.
var (
	stealthCounter  = regexp.MustCompile(`^\$\{name\}/\$\{version\}_([0-9]+)$`)
	stealthRevision = `${name}/${version}_${revision}`
)

// DistSubdir is how a stealth update's dist_subdir names the directory.
type DistSubdir struct {
	// ByRevision is ${name}/${version}_${revision}; otherwise Counter
	// numbers it.
	ByRevision bool
	Counter    int
}

// StealthDistSubdir keeps a distfile that changed upstream without a new
// name apart from the old one, so mirrors keep both. With the revision
// bumped, it sets dist_subdir ${name}/${version}_${revision} after the
// checksums; without, ${name}/${version}_1. One already numbered counts
// up, bumped or not: switching it to the revision could name the old
// archive's directory. One that follows the revision needs the bump. A
// dist_subdir set any other way, or inside a block, is the person's to
// change.
func StealthDistSubdir(src []byte, revbumped bool) ([]byte, DistSubdir, error) {
	script, subdirs, checksums, err := distSubdirCommands(src)
	if err != nil {
		return nil, DistSubdir{}, err
	}
	switch {
	case len(subdirs) > 1:
		return nil, DistSubdir{}, fmt.Errorf("%w: dist_subdir is set more than once", ErrUnsupported)
	case len(subdirs) == 1:
		cmd := subdirs[0]
		if len(cmd.Words) != 2 {
			return nil, DistSubdir{}, fmt.Errorf("%w: dist_subdir is not one word", ErrUnsupported)
		}
		value := string(src[cmd.Words[1].Span.Start:cmd.Words[1].Span.End])
		if value == stealthRevision {
			if !revbumped {
				return nil, DistSubdir{}, fmt.Errorf("%w: dist_subdir follows the revision, so without a revision bump the new archive would land where the old one is", ErrUnsupported)
			}
			return src, DistSubdir{ByRevision: true}, nil
		}
		m := stealthCounter.FindStringSubmatch(value)
		if m == nil {
			return nil, DistSubdir{}, fmt.Errorf("%w: dist_subdir is already set, to %s", ErrUnsupported, value)
		}
		n, _ := strconv.Atoi(m[1])
		next := fmt.Sprintf("${name}/${version}_%d", n+1)
		out, err := text.Apply(src, []text.Edit{{Span: cmd.Words[1].Span, New: []byte(next)}})
		return out, DistSubdir{Counter: n + 1}, err
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
	value, form := "${name}/${version}_1", DistSubdir{Counter: 1}
	if revbumped {
		value, form = stealthRevision, DistSubdir{ByRevision: true}
	}
	line := "dist_subdir" + strings.Repeat(" ", width-len("dist_subdir")) + value
	insert := newline + line
	if at == script.Span.End && at > 0 && src[at-1] == '\n' {
		insert = line + newline
	}
	out, err := text.Apply(src, []text.Edit{{Span: text.Span{Start: at, End: at}, New: []byte(insert)}})
	return out, form, err
}

// RemoveStealthDistSubdir removes a stealth update's dist_subdir, in
// either form, once the version it was for has gone: a new version's
// archive has a name of its own. Any other dist_subdir stays.
func RemoveStealthDistSubdir(src []byte) ([]byte, bool, error) {
	_, subdirs, _, err := distSubdirCommands(src)
	if err != nil || len(subdirs) != 1 || len(subdirs[0].Words) != 2 {
		return src, false, err
	}
	cmd := subdirs[0]
	value := string(src[cmd.Words[1].Span.Start:cmd.Words[1].Span.End])
	if value != stealthRevision && !stealthCounter.MatchString(value) {
		return src, false, nil
	}
	start := bytes.LastIndexByte(src[:cmd.Span.Start], '\n') + 1
	end := cmd.Span.End
	if i := bytes.IndexByte(src[end:], '\n'); i >= 0 {
		end += i + 1
	} else {
		end = len(src)
	}
	if strings.TrimSpace(string(src[start:cmd.Span.Start])) != "" || strings.TrimSpace(string(src[cmd.Span.End:end])) != "" {
		return src, false, nil
	}
	out, err := text.Apply(src, []text.Edit{{Span: text.Span{Start: start, End: end}}})
	return out, err == nil, err
}

// distSubdirCommands are the top-level dist_subdir and checksums commands.
func distSubdirCommands(src []byte) (*syntax.Script, []syntax.Command, []syntax.Command, error) {
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return nil, nil, nil, fmt.Errorf("%w: invalid Tcl syntax: %v", ErrUnsupported, errs)
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
	return script, subdirs, checksums, nil
}
