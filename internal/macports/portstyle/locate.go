package portstyle

import (
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// Located is a field whose carrying span is verified by construction: the
// span's text — through the style's declared transform, identity for most
// styles — equals the value evaluation reported for the context. Span
// therefore carries the literal form, which is the edit target (a bump
// writes the new upstream literal); Value is the evaluated form, which is
// what a predicted delta must contain.
type Located struct {
	Field info.Field
	Style Type
	Span  text.Span
	Value string
}

// scriptBodied names the commands whose braced arguments are scripts that
// run during evaluation, and are therefore searched for styles. Bodies
// that only might run — proc definitions, phase blocks like pre-configure —
// stay out: a version set there is not the evaluated version.
var scriptBodied = map[string]bool{
	"if":       true,
	"platform": true,
	"variant":  true,
	"foreach":  true,
	"while":    true,
}

// Locate finds the span carrying the given field's value for one
// evaluation context. The tree must be a clean parse of src; vals must be
// that context's evaluated state. On failure the returned error is a
// *Decline.
//
// Scopes searched: top-level commands, plus the bodies of subport blocks
// whose name is the context's — a subport's overrides live in its block,
// and a context without an override inherits the top-level style, which
// is then the span that carries its value.
func Locate(src []byte, tree *syntax.Script, vals info.Values, field info.Field) (Located, error) {
	if field != info.FieldVersion {
		return Located{}, &Decline{Type: FieldUnsupported, Field: field}
	}
	value := vals.Version

	type candidate struct {
		style     Type
		span      text.Span
		literal   bool
		transform func(string) string
	}
	var candidates []candidate

	var collect func(sc *syntax.Script)
	collect = func(sc *syntax.Script) {
		for _, it := range sc.Items {
			cmd, ok := it.(syntax.Command)
			if !ok {
				continue
			}
			name, ok := cmd.Name(src)
			if !ok {
				continue
			}
			for _, vc := range versionStyles {
				if name != vc.command || len(cmd.Words) <= vc.word {
					continue
				}
				w := cmd.Words[vc.word]
				_, lit := w.Literal(src)
				candidates = append(candidates, candidate{vc.style, w.Span, lit, vc.transform})
			}
			// Conditional scopes: descend into the brace bodies of
			// script-bodied commands. Corroboration makes this safe —
			// evaluation already resolved the conditionals on this host,
			// so only the taken branch's span can match the evaluated
			// value. The whitelist is the safety boundary: descending
			// into arbitrary braced words would read prose (a
			// long_description mentioning a version) as candidates.
			if scriptBodied[name] {
				for _, w := range cmd.Words[1:] {
					if body, ok := w.BracedScript(src); ok {
						collect(body)
					}
				}
			}
		}
	}

	collect(tree)
	for _, it := range tree.Items {
		cmd, ok := it.(syntax.Command)
		if !ok {
			continue
		}
		if name, ok := cmd.Name(src); !ok || name != "subport" || len(cmd.Words) < 3 {
			continue
		}
		if cmd.Words[1].Span.Text(src) != vals.Name {
			continue
		}
		body, ok := cmd.Words[2].BracedScript(src)
		if !ok {
			continue
		}
		collect(body)
	}

	if len(candidates) == 0 {
		return Located{}, &Decline{Type: UnknownStyle, Field: field}
	}

	// Corroborate, preferring the last match in document order: Tcl's
	// later assignment wins, so the last span whose text equals the
	// evaluated value is the one that produced it.
	best := -1
	for i, c := range candidates {
		got := c.span.Text(src)
		if c.transform != nil {
			got = c.transform(got)
		}
		if c.literal && value != "" && got == value {
			if best < 0 || c.span.Start > candidates[best].span.Start {
				best = i
			}
		}
	}
	if best < 0 {
		d := &Decline{Type: NotLiteral, Field: field}
		for _, c := range candidates {
			d.Candidates = append(d.Candidates, c.span)
		}
		return Located{}, d
	}
	c := candidates[best]
	return Located{Field: field, Style: c.style, Span: c.span, Value: value}, nil
}
