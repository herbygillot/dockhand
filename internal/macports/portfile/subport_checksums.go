package portfile

import (
	"bytes"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// fetchCommands are the commands that say what a port fetches, after the
// last of which a subport's checksums are written, as create orders them.
var fetchCommands = func() map[string]bool {
	commands := map[string]bool{
		"version": true, "distname": true, "distfiles": true, "dist_subdir": true,
		"master_sites": true, "master_sites.mirror_subdir": true, "extract.suffix": true,
		"github.setup": true, "github.tarball_from": true, "gitlab.setup": true, "codeberg.setup": true, "bitbucket.setup": true,
	}
	for _, option := range macports.ExtractOptions {
		commands[option] = true
	}
	return commands
}()

// DeclareSubportChecksums writes a checksums declaration of placeholders
// for these archives in a subport's block, which declares none, after its
// last fetch line, as create writes one: rmd160, sha256, and size, each
// zero, for a refresh to fill in, and each archive named where there are
// several. kb-old's block, a subport at 0.1.6 beside kb, declared none, and
// kb's declaration names kb's own archive, so checksums kb-old was refused
// (field testing on rc3). False where the subport has no one block with a
// literal name, its block declares checksums already, or its lines aren't
// one command to a line.
func DeclareSubportChecksums(src []byte, subport string, names []string) ([]byte, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 || len(names) == 0 {
		return nil, false
	}
	body, ok := subportBody(src, script, subport)
	if !ok {
		return nil, false
	}
	var anchor, last *syntax.Command
	for _, item := range body.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		name, _ := cmd.Name(src)
		switch name {
		case "checksums", "checksums-append", "checksums-prepend":
			return nil, false
		}
		if fetchCommands[strings.TrimSuffix(strings.TrimSuffix(name, "-append"), "-prepend")] {
			anchor = &cmd
		}
		last = &cmd
	}
	if anchor == nil {
		anchor = last
	}
	if anchor == nil {
		return nil, false
	}
	start := bytes.LastIndexByte(src[:anchor.Span.Start], '\n') + 1
	indent := string(src[start:anchor.Span.Start])
	if strings.Trim(indent, " \t") != "" {
		return nil, false
	}
	end := anchor.Span.End
	if newline := bytes.IndexByte(src[end:], '\n'); newline >= 0 {
		end += newline
	} else {
		end = len(src)
	}
	if strings.TrimSpace(string(src[anchor.Span.End:end])) != "" {
		return nil, false
	}
	var lines []string
	for _, name := range names {
		if len(names) > 1 {
			lines = append(lines, name)
		}
		lines = append(lines, "rmd160  0", "sha256  0", "size    0")
	}
	declaration := "\n" + indent + "checksums           " + strings.Join(lines, " \\\n"+indent+strings.Repeat(" ", 20))
	out, err := textedit.Apply(src, []textedit.Edit{{Span: textedit.Span{Start: end, End: end}, New: []byte(declaration)}})
	return out, err == nil
}

// BlockPlace is where a command lies against a subport's block.
type BlockPlace int

const (
	// PlaceUnknown is a subport with no one literal block, or no such
	// command.
	PlaceUnknown BlockPlace = iota
	PlaceInBlock
	PlaceBeforeBlock
	PlaceAfterBlock
)

// SubportPlace is where the command at ordinal, counted as Commands visits
// every command, lies against subport's one block with a literal name: a
// checksums declaration of the port's before the block, which a
// declaration in the block replaces, or after it, which MacPorts reads
// over the block's.
func SubportPlace(src []byte, subport string, ordinal int) BlockPlace {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 || ordinal < 0 {
		return PlaceUnknown
	}
	var blocks []textedit.Span
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		if name, _ := cmd.Name(src); name != "subport" || len(cmd.Words) != 3 {
			continue
		}
		if name, ok := cmd.Words[1].Literal(src); ok && name == subport {
			blocks = append(blocks, cmd.Span)
		}
	}
	if len(blocks) != 1 {
		return PlaceUnknown
	}
	block := blocks[0]
	index := 0
	for cmd := range script.Commands(src, func(syntax.Command) bool { return true }) {
		if index++; index-1 != ordinal {
			continue
		}
		switch {
		case cmd.Span.Start >= block.Start && cmd.Span.End <= block.End:
			return PlaceInBlock
		case cmd.Span.End <= block.Start:
			return PlaceBeforeBlock
		case cmd.Span.Start >= block.End:
			return PlaceAfterBlock
		}
		return PlaceUnknown
	}
	return PlaceUnknown
}

// DeclareChecksums writes a checksums declaration of placeholders for
// these archives at a Portfile's top, which declares none, after its last
// fetch line there, aligned as that line's value is and set off by a
// blank line: rmd160, sha256, and size, each zero, for a refresh to fill
// in, as DeclareSubportChecksums writes a subport's. False where the top
// declares checksums already, has no fetch line, or the line isn't one
// command to a line.
func DeclareChecksums(src []byte, names []string) ([]byte, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 || len(names) == 0 {
		return nil, false
	}
	var anchor *syntax.Command
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		name, _ := cmd.Name(src)
		switch name {
		case "checksums", "checksums-append", "checksums-prepend":
			return nil, false
		}
		if fetchCommands[strings.TrimSuffix(strings.TrimSuffix(name, "-append"), "-prepend")] {
			anchor = &cmd
		}
	}
	if anchor == nil || strings.TrimSpace(string(src[lineStart(src, anchor.Span.Start):anchor.Span.Start])) != "" {
		return nil, false
	}
	end := lineEnd(src, anchor.Span.End)
	if strings.TrimSpace(string(src[anchor.Span.End:end])) != "" {
		return nil, false
	}
	column := max(valueColumn(src, *anchor), len("checksums")+1)
	indent := strings.Repeat(" ", column)
	var lines []string
	for _, name := range names {
		if len(names) > 1 {
			lines = append(lines, name)
		}
		lines = append(lines, "rmd160  0", "sha256  0", "size    0")
	}
	declaration := "\n\nchecksums" + strings.Repeat(" ", column-len("checksums")) + strings.Join(lines, " \\\n"+indent)
	if next := end + 1; next < len(src) && src[next] != '\n' {
		declaration += "\n"
	}
	out, err := textedit.Apply(src, []textedit.Edit{{Span: textedit.Span{Start: end, End: end}, New: []byte(declaration)}})
	return out, err == nil
}

// NameChecksums rewrites a Portfile's top checksums declaration of one
// archive, unnamed, as one of placeholders for each of these archives,
// named, as DeclareChecksums writes one, for a refresh to fill in: a port
// that came to fetch several archives, as shader-slang's six, kept its one
// "checksums rmd160 0 sha256 0 size 0", and checksums refused it (field
// testing, #66). False where the top has no such one declaration, its
// values aren't literal, or its lines hold another command.
func NameChecksums(src []byte, names []string) ([]byte, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) > 0 || len(names) < 2 {
		return nil, false
	}
	var found *syntax.Command
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		switch name, _ := cmd.Name(src); name {
		case "checksums-append", "checksums-prepend":
			return nil, false
		case "checksums":
			if found != nil {
				return nil, false
			}
			found = &cmd
		}
	}
	if found == nil || len(found.Words) < 3 {
		return nil, false
	}
	first, ok := found.Words[1].Literal(src)
	if !ok || !slices.Contains([]string{"rmd160", "sha256", "sha1", "md5", "size"}, first) {
		return nil, false
	}
	start, end := lineStart(src, found.Span.Start), lineEnd(src, found.Span.End)
	if strings.TrimSpace(string(src[start:found.Span.Start])) != "" || strings.TrimSpace(string(src[found.Span.End:end])) != "" {
		return nil, false
	}
	column := max(valueColumn(src, *found), len("checksums")+1)
	var lines []string
	for _, name := range names {
		lines = append(lines, name, "rmd160  0", "sha256  0", "size    0")
	}
	declaration := "checksums" + strings.Repeat(" ", column-len("checksums")) + strings.Join(lines, " \\\n"+strings.Repeat(" ", column))
	out, err := textedit.Apply(src, []textedit.Edit{{Span: textedit.Span{Start: start, End: end}, New: []byte(declaration)}})
	return out, err == nil
}
