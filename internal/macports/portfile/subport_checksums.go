package portfile

import (
	"bytes"
	"strings"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// fetchCommands are the commands that say what a port fetches, after the
// last of which a subport's checksums are written, as create orders them.
var fetchCommands = map[string]bool{
	"version": true, "distname": true, "distfiles": true, "dist_subdir": true,
	"master_sites": true, "master_sites.mirror_subdir": true, "extract.suffix": true,
	"use_bzip2": true, "use_xz": true, "use_zip": true, "use_7z": true, "use_lzip": true,
	"github.setup": true, "github.tarball_from": true, "gitlab.setup": true, "codeberg.setup": true, "bitbucket.setup": true,
}

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
