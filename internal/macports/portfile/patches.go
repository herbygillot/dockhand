package portfile

import (
	"bytes"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

// DropPatch takes a patch file's name out of the Portfile's top-level
// patchfiles or patchfiles-append, where it's written as a literal word,
// and the command with it where it was the command's only word: what a
// maintainer does when upstream merges a patch. Its second result is
// false, and the Portfile as it was, where the name isn't written so: in
// a variant, a condition, or a computed word, which a person drops.
func DropPatch(src []byte, name string) ([]byte, bool) {
	script, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return src, false
	}
	var found []textedit.Edit
	for _, cmd := range script.Direct() {
		command, ok := cmd.Name(src)
		if !ok || command != "patchfiles" && command != "patchfiles-append" {
			continue
		}
		for i, word := range cmd.Words[1:] {
			value, literal := word.Literal(src)
			if !literal || value != name {
				continue
			}
			if len(cmd.Words) == 2 {
				// The whole line goes, with its line end.
				start := bytes.LastIndexByte(src[:cmd.Span.Start], '\n') + 1
				end := cmd.Span.End
				if end < len(src) && src[end] == '\n' {
					end++
				}
				found = append(found, textedit.Edit{Span: textedit.Span{Start: start, End: end}})
				continue
			}
			// The word goes, with what separates it from the word before
			// it, a line continuation included.
			previous := cmd.Words[i]
			found = append(found, textedit.Edit{Span: textedit.Span{Start: previous.Span.End, End: word.Span.End}})
		}
	}
	if len(found) != 1 {
		return src, false
	}
	out, err := textedit.Apply(src, found)
	if err != nil {
		return src, false
	}
	return out, true
}
