package portfile

import (
	"bytes"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// SourceMove is the edit that moves a GitHub port's source to a release's
// own file or to GitHub's archive of the tag: github.tarball_from's
// literal rewritten to Mode, and, for a release asset whose name, suffix,
// or top directory isn't the github PortGroup's default, the lines that
// say it, each a Tcl word written as given. FromGit moves a port fetched
// with Git for its submodules: fetch.type git and the submodule hook go,
// and a checksums declaration of placeholders follows the block, for the
// refresh to fill as a subport's is.
type SourceMove struct {
	// Mode is releases or archive.
	Mode string
	// Distname, Suffix, and Worksrcdir are distname, extract.suffix, and
	// worksrcdir's values; UseOption is the use_ option the asset's
	// compression takes, use_bzip2 or use_xz, written yes. Each is
	// written only where it isn't empty.
	Distname, UseOption, Suffix, Worksrcdir string
	// Command and Depends are extract.cmd and the depends_extract-append
	// an archive no use_ option extracts takes, zstd's (macports.Extraction).
	Command, Depends string
	FromGit          bool
	// Comment are lines written above the block, each without its "# ";
	// none is written where it's empty (the roadmap's D3).
	Comment []string
}

// lines are the block's option lines, in the order written.
func (m SourceMove) lines() [][2]string {
	var lines [][2]string
	for _, line := range [][2]string{{"distname", m.Distname}, {m.UseOption, "yes"}, {"extract.suffix", m.Suffix}, {"extract.cmd", m.Command}, {"depends_extract-append", m.Depends}, {"worksrcdir", m.Worksrcdir}} {
		if line[0] != "" && line[1] != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// sourceModes are the github.tarball_from values a move rewrites, and the
// ones it writes.
var sourceModes = []string{"tarball", "archive", "releases"}

// MoveSource is a Portfile's text with the move made, at its top alone: a
// port whose github.setup, github.tarball_from, fetch.type, or hook is set
// anywhere else, as in a subport's block or an if, or set twice, is the
// maintainer's to move, and false. Where the Portfile sets no
// github.tarball_from, none is added for releases, the PortGroup's
// default. The PortGroup's stock comment above github.tarball_from goes
// with it. The block follows the setup lines, github.setup and the
// github.tarball_from and revision lines right after it, with a blank
// line on each side; a moved Git port's fetch.type and hook go whole, the
// comment lines right above each with it, and a blank line doubled by
// that is collapsed. A Git port qualifies only where its one post-fetch
// or post-extract hook is the submodule one (SubmoduleHook), and it
// declares no checksums.
func MoveSource(src []byte, move SourceMove) ([]byte, bool) {
	if !slices.Contains(sourceModes[1:], move.Mode) {
		return src, false
	}
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return src, false
	}
	named := map[string][]syntax.Command{}
	nested := map[string]bool{}
	commands(src, script, false, func(cmd syntax.Command, inside bool) {
		name, _ := cmd.Name(src)
		switch name {
		case "github.setup", "github.tarball_from", "fetch.type", "post-fetch", "post-extract", "checksums", "checksums-append", "checksums-prepend":
			named[name] = append(named[name], cmd)
			nested[name] = nested[name] || inside
		}
	})
	scoped := []string{"github.setup", "github.tarball_from", "fetch.type"}
	if move.FromGit {
		scoped = append(scoped, "post-fetch", "post-extract")
	}
	for _, name := range scoped {
		if nested[name] || len(named[name]) > 1 {
			return src, false
		}
	}
	if len(named["github.setup"]) != 1 {
		return src, false
	}
	var edits []textedit.Edit
	var from *syntax.Command
	switch found := named["github.tarball_from"]; len(found) {
	case 0:
		if move.Mode != "releases" {
			return src, false
		}
	default:
		cmd := found[0]
		value, ok := "", len(cmd.Words) == 2
		if ok {
			value, ok = cmd.Words[1].Literal(src)
		}
		if !ok || !slices.Contains(sourceModes, value) {
			return src, false
		}
		from = &cmd
		if value != move.Mode {
			edits = append(edits, textedit.Edit{Span: cmd.Words[1].Span, New: []byte(move.Mode)})
		}
		start := lineStart(src, cmd.Span.Start)
		if start > 0 {
			above := lineStart(src, start-1)
			if stockComment.Match(src[above : start-1]) {
				edits = append(edits, textedit.Edit{Span: textedit.Span{Start: above, End: start}})
			}
		}
	}
	if move.FromGit {
		fetch := named["fetch.type"]
		if len(fetch) != 1 || len(fetch[0].Words) != 2 || len(named["checksums"]) > 0 || len(named["checksums-append"]) > 0 || len(named["checksums-prepend"]) > 0 {
			return src, false
		}
		if value, ok := fetch[0].Words[1].Literal(src); !ok || value != "git" {
			return src, false
		}
		hooks := append(slices.Clone(named["post-fetch"]), named["post-extract"]...)
		if len(hooks) != 1 || len(hooks[0].Words) != 2 {
			return src, false
		}
		body, ok := hooks[0].Words[1].BracedScript(src)
		if !ok || !SubmoduleHook(string(src[body.Span.Start:body.Span.End])) {
			return src, false
		}
		for _, cmd := range []syntax.Command{fetch[0], hooks[0]} {
			span, ok := wholeLines(src, cmd.Span)
			if !ok {
				return src, false
			}
			edits = append(edits, textedit.Edit{Span: span})
		}
	} else if len(named["fetch.type"]) > 0 {
		return src, false
	}
	block := blockText(src, named["github.setup"][0], move)
	if block != "" {
		anchor := setupEnd(src, script, named["github.setup"][0], from)
		end := lineEnd(src, anchor.Span.End)
		if strings.TrimSpace(string(src[anchor.Span.End:end])) != "" {
			return src, false
		}
		text := "\n\n" + block
		if next := end + 1; next < len(src) && src[next] != '\n' {
			text += "\n"
		}
		edits = append(edits, textedit.Edit{Span: textedit.Span{Start: end, End: end}, New: []byte(text)})
	}
	if len(edits) == 0 {
		return src, true
	}
	out, err := textedit.Apply(src, edits)
	return out, err == nil
}

// blockText is the block a move writes, its values aligned where
// github.setup's are, and for a moved Git port its checksums of
// placeholders after it; empty where there's neither.
func blockText(src []byte, setup syntax.Command, move SourceMove) string {
	column := valueColumn(src, setup)
	line := func(option, value string) string {
		return option + strings.Repeat(" ", max(column-len(option), 1)) + value
	}
	var parts []string
	var block []string
	for _, comment := range move.Comment {
		block = append(block, strings.TrimRight("# "+comment, " "))
	}
	for _, option := range move.lines() {
		block = append(block, line(option[0], option[1]))
	}
	if len(move.lines()) > 0 {
		parts = append(parts, strings.Join(block, "\n"))
	}
	if move.FromGit {
		indent := strings.Repeat(" ", max(column, len("checksums")+1))
		parts = append(parts, line("checksums", "rmd160  0 \\\n"+indent+"sha256  0 \\\n"+indent+"size    0"))
	}
	return strings.Join(parts, "\n\n")
}

// valueColumn is where a command's value starts on its line, the column a
// Portfile aligns its values at.
func valueColumn(src []byte, cmd syntax.Command) int {
	if len(cmd.Words) < 2 {
		return 24
	}
	return cmd.Words[1].Span.Start - lineStart(src, cmd.Span.Start)
}

// setupEnd is the last of the setup lines: github.setup, then the
// github.tarball_from and revision lines that follow it with no blank line
// between.
func setupEnd(src []byte, script *syntax.Script, setup syntax.Command, from *syntax.Command) syntax.Command {
	anchor := setup
	following := false
	for _, item := range script.Items {
		cmd, ok := item.(syntax.Command)
		if !ok {
			continue
		}
		if cmd.Span == setup.Span {
			following = true
			continue
		}
		if !following {
			continue
		}
		name, _ := cmd.Name(src)
		between := src[anchor.Span.End:lineStart(src, cmd.Span.Start)]
		if name != "revision" && (from == nil || cmd.Span != from.Span) || bytes.Contains(between, []byte("\n\n")) || bytes.Count(between, []byte("\n")) > 1 && !onlyComments(between) {
			break
		}
		anchor = cmd
	}
	return anchor
}

// onlyComments reports text whose lines past the first are each a comment.
func onlyComments(text []byte) bool {
	lines := strings.Split(string(text), "\n")
	for _, line := range lines[1:] {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return false
		}
	}
	return true
}

// lineStart is where the line holding offset starts.
func lineStart(src []byte, offset int) int {
	return bytes.LastIndexByte(src[:offset], '\n') + 1
}

// lineEnd is the newline ending the line holding offset, or the text's end.
func lineEnd(src []byte, offset int) int {
	if newline := bytes.IndexByte(src[offset:], '\n'); newline >= 0 {
		return offset + newline
	}
	return len(src)
}

// wholeLines is the span of a command's lines with their newline, the
// comment lines right above it, and, where it stood between two blank
// lines, one of them; false where the lines hold anything else.
func wholeLines(src []byte, span textedit.Span) (textedit.Span, bool) {
	start, end := lineStart(src, span.Start), lineEnd(src, span.End)
	if strings.TrimSpace(string(src[start:span.Start])) != "" || strings.TrimSpace(string(src[span.End:end])) != "" {
		return textedit.Span{}, false
	}
	for start > 0 {
		above := lineStart(src, start-1)
		if !strings.HasPrefix(strings.TrimSpace(string(src[above:start-1])), "#") {
			break
		}
		start = above
	}
	if end < len(src) {
		end++
	}
	blankBefore := start == 0 || start >= 2 && src[start-1] == '\n' && src[start-2] == '\n'
	if blankBefore && end < len(src) && src[end] == '\n' {
		end++
	}
	return textedit.Span{Start: start, End: end}, true
}
