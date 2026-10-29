package portfile

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// Rename sets the port name a Portfile declares from old to name where the
// Portfile sets it: its name line's literal, else the project of the forge
// setup the name comes from, github.setup's and its kin's second word, or
// the last element of go.setup's package, as field testing's hand rename
// of goplus left xgo's name to go.setup (2026-10-29). Each is the port's
// own, at the top of the Portfile, never a subport's. A name set any other
// way is refused, as is one whose words aren't literals.
func Rename(src []byte, old, name string) ([]byte, error) {
	if !Literal(name) {
		return nil, ErrUnsupported.Because("%q is no name a Portfile writes as a literal", name)
	}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, ErrUnsupported.Because("invalid Tcl syntax")
	}
	var named, setup, golang *textedit.Edit
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		command, _ := cmd.Name(src)
		switch {
		case command == "name" && len(cmd.Words) == 2:
			if value, literal := cmd.Words[1].Literal(src); literal && value == old {
				named = &textedit.Edit{Span: cmd.Words[1].Span, New: []byte(name)}
			}
		case command == "go.setup" && len(cmd.Words) >= 3:
			if value, literal := cmd.Words[1].Literal(src); literal && path.Base(value) == old {
				golang = &textedit.Edit{Span: cmd.Words[1].Span, New: []byte(path.Join(path.Dir(value), name))}
			}
		case strings.HasSuffix(command, ".setup") && len(cmd.Words) >= 4:
			if value, literal := cmd.Words[2].Literal(src); literal && value == old {
				setup = &textedit.Edit{Span: cmd.Words[2].Span, New: []byte(name)}
			}
		}
	}
	for _, edit := range []*textedit.Edit{named, setup, golang} {
		if edit != nil {
			return textedit.Apply(src, []textedit.Edit{*edit})
		}
	}
	return nil, ErrUnsupported.Because("the Portfile sets the name %s neither on a name line nor through a forge's setup, as literals; rename it there by hand", old)
}

// SetPrimaryCategory names category first on the port's categories line
// where from is first now, as a port moved to another category's directory
// is. It's left as it is, false, where no categories line names from first
// as a literal, as where a PortGroup sets the categories.
func SetPrimaryCategory(src []byte, from, category string) ([]byte, bool, error) {
	if from == category {
		return src, false, nil
	}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return nil, false, ErrUnsupported.Because("invalid Tcl syntax")
	}
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		if command, _ := cmd.Name(src); command != "categories" || len(cmd.Words) < 2 {
			continue
		}
		first, literal := cmd.Words[1].Literal(src)
		if !literal || first != from {
			return src, false, nil
		}
		out, err := textedit.Apply(src, []textedit.Edit{{Span: cmd.Words[1].Span, New: []byte(category)}})
		return out, err == nil, err
	}
	return src, false, nil
}

// Stub is the Portfile a renamed port leaves at its old name: PortGroup
// obsolete 1.0, replaced_by its new name, at its version with its revision
// raised, which replaced-by-unchanged asks, its epoch kept, in its
// categories, as MacPorts renames a port.
type Stub struct {
	Name, ReplacedBy, Version string
	Revision, Epoch           int
	Categories                []string
}

// Text is the stub's Portfile.
func (s Stub) Text() []byte {
	var b strings.Builder
	line := func(option, value string) { fmt.Fprintf(&b, "%-20s%s\n", option, value) }
	b.WriteString(Modeline + "\n\n")
	line("PortSystem", "1.0")
	line("PortGroup", "obsolete 1.0")
	b.WriteString("\n")
	line("name", s.Name)
	line("replaced_by", s.ReplacedBy)
	if s.Epoch > 0 {
		line("epoch", strconv.Itoa(s.Epoch))
	}
	line("version", s.Version)
	line("revision", strconv.Itoa(s.Revision))
	if len(s.Categories) > 0 {
		line("categories", strings.Join(s.Categories, " "))
	}
	return []byte(b.String())
}
