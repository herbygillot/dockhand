// Package text holds the primitives for byte-exact source manipulation:
// spans, positions, and edit application. It is a leaf package — everything
// that touches source bytes (the Tcl parser, recognizers, edit planning)
// builds on these types, and this package depends on nothing.
//
// Edits return new bytes only. Rendering a diff is deliberately not
// provided: the reviewable diff is git's job, because git's rendering is
// what reviewers actually see, and a second formatter is a second opinion.
package text

// Span is a half-open byte range [Start, End) into a source buffer. Nodes,
// edits, and errors all locate themselves with spans, so a tree or an edit
// list plus the original source is always sufficient to reconstruct or
// transform the input exactly.
type Span struct {
	Start int
	End   int
}

// Bytes returns the source bytes the span covers.
func (s Span) Bytes(src []byte) []byte { return src[s.Start:s.End] }

// Text returns the source bytes the span covers, as a string.
func (s Span) Text(src []byte) string { return string(src[s.Start:s.End]) }

// Len returns the span's length in bytes.
func (s Span) Len() int { return s.End - s.Start }

// Position converts a byte offset into a 1-based line and column, for
// presenting spans to humans. Columns count bytes, not display width.
func Position(src []byte, offset int) (line, col int) {
	line, col = 1, 1
	for i := 0; i < offset && i < len(src); i++ {
		if src[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}
