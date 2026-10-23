package portfile

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// LocateDeclaration requires a unique source command inside the observed Tcl
// frame. Dynamic or ambiguous source locations are not editable.
func LocateDeclaration(src []byte, path string, declaration macports.Declaration) (syntax.Command, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	script, errs := syntax.Parse(src)
	if len(errs) > 0 {
		return syntax.Command{}, ErrUnsupported.Because("invalid Tcl")
	}
	var commands []syntax.Command
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		commands = append(commands, cmd)
	}

	if group, ok := portGroupFrame(path, declaration); ok {
		return syntax.Command{}, group
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
		return syntax.Command{}, ErrUnsupported.Because("declaration has no source location in the Portfile")
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
				return syntax.Command{}, ErrUnsupported.Because("declaration belongs to another source file")
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
				line, _ := textedit.Position(src, scope.Span.Start)
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
	// Of commands of the same text, the one a frame of this file places
	// is the one that ran: LyX's two blocks of the same checksums, in two
	// branches of an if, each on its own line (the roadmap's v0.5.0 part
	// 3, step 2).
	if len(matches) > 1 {
		var placed []syntax.Command
		for _, cmd := range matches {
			line, _ := textedit.Position(src, cmd.Span.Start)
			for _, frame := range declaration.Frames {
				if filepath.Clean(frame.File) == filepath.Clean(path) && frame.Line == line && sourceCommand(frame.Command) == sourceCommand(cmd.Span.Text(src)) {
					placed = append(placed, cmd)
					break
				}
			}
		}
		if len(placed) == 1 {
			return placed[0], nil
		}
	}
	switch len(matches) {
	case 0:
		return syntax.Command{}, ErrUnsupported.Because("no command written in the Portfile makes it, as an eval'd one isn't")
	case 1:
		return matches[0], nil
	}
	return syntax.Command{}, &AmbiguousDeclaration{Matches: matches}
}

// AmbiguousDeclaration is a declaration more than one command in the
// Portfile could have made, by their text, as LyX's two checksums blocks
// of the same words in two branches: which one ran is for an evaluation
// to say (portedit's probe), not the text.
type AmbiguousDeclaration struct {
	Matches []syntax.Command
}

func (a *AmbiguousDeclaration) Error() string {
	return fmt.Sprintf("%v: %s", ErrUnsupported, a.Words())
}

// Words are why it's refused, without ErrUnsupported's (refusal.WordsOf).
func (a *AmbiguousDeclaration) Words() string {
	return fmt.Sprintf("declaration source is ambiguous (%d matches)", len(a.Matches))
}

func (a *AmbiguousDeclaration) Unwrap() error { return ErrUnsupported }

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
			return syntax.Command{}, true, ErrUnsupported.Because("variant %s makes the declaration through another procedure", variant)
		}
		inner := sourceCommand(declaration.Frames[i+1].Command)
		var bodies []textedit.Span
		for _, cmd := range commands {
			if command, _ := cmd.Name(src); command != "variant" || len(cmd.Words) < 3 {
				continue
			}
			if named, ok := cmd.Words[1].Literal(src); ok && named == variant {
				bodies = append(bodies, cmd.Words[len(cmd.Words)-1].Span)
			}
		}
		if len(bodies) != 1 {
			return syntax.Command{}, true, ErrUnsupported.Because("the Portfile defines variant %s %d times", variant, len(bodies))
		}
		var matches []syntax.Command
		for _, cmd := range commands {
			command, _ := cmd.Name(src)
			if command == name && cmd.Span.Start >= bodies[0].Start && cmd.Span.End <= bodies[0].End && sourceCommand(cmd.Span.Text(src)) == inner {
				matches = append(matches, cmd)
			}
		}
		if len(matches) != 1 {
			return syntax.Command{}, true, ErrUnsupported.Because("variant %s's declaration source is ambiguous (%d matches)", variant, len(matches))
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
		return nil, ErrUnsupported.Because("invalid Portfile syntax")
	}
	var edits []textedit.Edit
	for cmd := range script.Commands(contents, func(syntax.Command) bool { return true }) {
		if name, _ := cmd.Name(contents); name != command || len(cmd.Words) != 2 {
			continue
		}
		if literal, ok := cmd.Words[1].Literal(contents); ok && literal == old && !cmd.Words[1].Expand {
			edits = append(edits, textedit.Edit{Span: cmd.Words[1].Span, New: []byte(next)})
		}
	}
	if len(edits) != 1 {
		return nil, ErrUnsupported.Because("%s is %s but no single literal declaration carries it", command, old)
	}
	return textedit.Apply(contents, edits)
}

// InPortGroup is a declaration a PortGroup's own code made, as
// crossbinutils 1.0 writes each port's checksums from its table,
// crossbinutils.versions_info: the PortGroup's, at its file and line, and
// the tables its command reads. It's no Portfile's to edit, and a change
// there moves every port of the group (the roadmap's v0.5.0 part 3, step
// 2: 73 ports were told the checksums couldn't be found in the Portfile,
// and to paste a block into it).
type InPortGroup struct {
	// Group is the PortGroup as a Portfile names it, "crossbinutils 1.0";
	// File its file below the tree, as _resources/port1.0/group/...
	Group, File string
	Line        int
	Tables      []string
}

func (g *InPortGroup) Error() string { return fmt.Sprintf("%v: %s", ErrUnsupported, g.Words()) }

// Words are why it's refused, without ErrUnsupported's (refusal.WordsOf).
func (g *InPortGroup) Words() string {
	from := ""
	if len(g.Tables) > 0 {
		from = ", from " + strings.Join(g.Tables, " and ")
	}
	return fmt.Sprintf("set by the %s PortGroup, at %s:%d%s, which no Portfile edit changes; a change there moves every port of the group", g.Group, g.File, g.Line, from)
}

func (g *InPortGroup) Unwrap() error { return ErrUnsupported }

// WritesChecksums reports whether the Portfile writes every checksum a
// declaration carries, its values as checksums takes them ("file sha256
// <hex> size <n>"), as words of its own: rust 1.0's checksums-append
// carries each cargo.crates row's sha256, which the Portfile writes and an
// update regenerates, where crossbinutils 1.0's carries a row of its own
// table. A PortGroup's command that carries the Portfile's checksums is the
// Portfile's to change, though the PortGroup makes it: refused as the
// PortGroup's, every cargo port's update stopped (field testing on
// 5a727e00, purl, fakecloud, and wasmtime). A size is a value as a sum is,
// the Portfile's where it writes it. golang 1.0's go.vendors appends each
// row in three: its file alone, then each sum, then its size, so a
// declaration that carries a file and no value is no value the PortGroup
// sets (field testing's git-credential-oauth 0.18.0).
func WritesChecksums(src []byte, values []string) bool {
	var fields []string
	for _, value := range values {
		fields = append(fields, strings.Fields(value)...)
	}
	var sums []string
	for i := 0; i+1 < len(fields); i++ {
		switch fields[i] {
		case "sha256", "rmd160", "sha1", "md5", "size":
			sums = append(sums, fields[i+1])
			i++
		}
	}
	if len(sums) == 0 {
		return true
	}
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return false
	}
	written := map[string]bool{}
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		for _, word := range cmd.Words {
			if value, literal := word.Literal(src); literal {
				for _, field := range strings.Fields(value) {
					written[field] = true
				}
			}
		}
	}
	for _, sum := range sums {
		if !written[sum] {
			return false
		}
	}
	return true
}

// portGroupFile is a PortGroup's file, <name>-<version>.tcl in a
// port1.0/group directory.
var portGroupFile = regexp.MustCompile(`(?:^|/)(_resources/port1\.0/group/([A-Za-z0-9_+-]+)-([0-9][0-9.]*)\.tcl)$`)

// portTable is a Tcl array a command reads an element of, as [set
// crossbinutils.versions_info($version)] or ${name(key)}.
var portTable = regexp.MustCompile(`(?:\[set\s+|\$\{?)([A-Za-z_][A-Za-z0-9_.:]*)\(`)

// portGroupFrame reports the declaration as a PortGroup's, where the
// innermost frame of its command is a PortGroup's file, not the Portfile.
func portGroupFrame(path string, declaration macports.Declaration) (*InPortGroup, bool) {
	name := strings.TrimPrefix(declaration.Command, "::")
	command := -1
	for i := len(declaration.Frames) - 1; i >= 0; i-- {
		if firstWord(declaration.Frames[i].Command) == name {
			command = i
			break
		}
	}
	if command < 0 {
		return nil, false
	}
	// The command's own frame, else the nearest outside it that names a
	// file: a command in a body Tcl compiled apart, as crossgcc 1.0's
	// checksums inside an if of its procedure, has none of its own.
	for i := command; i >= 0; i-- {
		frame := declaration.Frames[i]
		if frame.File == "" {
			continue
		}
		if filepath.Clean(frame.File) == filepath.Clean(path) {
			return nil, false
		}
		match := portGroupFile.FindStringSubmatch(filepath.ToSlash(frame.File))
		if match == nil {
			return nil, false
		}
		group := &InPortGroup{Group: match[2] + " " + match[3], File: match[1], Line: frame.Line}
		for _, table := range portTable.FindAllStringSubmatch(declaration.Frames[command].Command, -1) {
			if !slices.Contains(group.Tables, table[1]) {
				group.Tables = append(group.Tables, table[1])
			}
		}
		return group, true
	}
	return nil, false
}
