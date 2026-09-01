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

// ScopeOf is the descend predicate for one evaluation context: the
// conditional scopes that run during evaluation, plus the body of the
// subport block whose name is the context's. Corroboration makes
// conditional descent safe — evaluation already resolved the
// conditionals on this host, so only the taken branch's span can match
// the evaluated value. The whitelist is the safety boundary: descending
// into arbitrary braced words would read prose (a long_description
// mentioning a version) as candidates.
func ScopeOf(src []byte, contextName string) func(syntax.Command) bool {
	return func(cmd syntax.Command) bool {
		name, ok := cmd.Name(src)
		if !ok {
			return false
		}
		if scriptBodied[name] {
			return true
		}
		return name == "subport" && len(cmd.Words) >= 3 &&
			cmd.Words[1].Span.Text(src) == contextName
	}
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
	var value string
	var styles []styleSpec
	switch field {
	case info.FieldVersion:
		value, styles = vals.Version, versionStyles
	case info.FieldRevision:
		value, styles = vals.Revision, revisionStyles
	case info.FieldName, info.FieldEpoch, info.FieldCategories,
		info.FieldLicense, info.FieldMaintainers, info.FieldPlatforms,
		info.FieldDescription, info.FieldHomepage, info.FieldLongDescription,
		info.FieldDistfiles, info.FieldChecksums,
		info.FieldDependsFetch, info.FieldDependsExtract,
		info.FieldDependsPatch, info.FieldDependsBuild,
		info.FieldDependsLib, info.FieldDependsRun, info.FieldDependsTest:
		return Located{}, &Decline{Type: FieldUnsupported, Field: field}
	}

	type candidate struct {
		style     Type
		span      text.Span
		literal   bool
		transform func(string) string
	}
	var candidates []candidate

	for cmd := range tree.Commands(src, ScopeOf(src, vals.Name)) {
		name, ok := cmd.Name(src)
		if !ok {
			continue
		}
		for _, vc := range styles {
			if name != vc.command || len(cmd.Words) <= vc.word {
				continue
			}
			w := cmd.Words[vc.word]
			_, lit := w.Literal(src)
			candidates = append(candidates, candidate{vc.style, w.Span, lit, vc.transform})
		}
	}

	if len(candidates) == 0 {
		return Located{}, &Decline{Type: UnknownStyle, Field: field}
	}

	// Corroborate, preferring the last match in document order: Tcl's
	// later assignment wins, so the last span whose text equals the
	// evaluated value is the one that produced it. SetVariable is the
	// exception both ways: a corroborated non-set carrier always
	// outranks it (any set whose value coincides with the version
	// corroborates, and the real style is the better claim), and it
	// never enters the decline's candidate list (the counterfactual
	// probe should not chase coincidental sets).
	best := -1
	outranks := func(i, j int) bool {
		if j < 0 {
			return true
		}
		a, b := candidates[i], candidates[j]
		if (a.style == SetVariable) != (b.style == SetVariable) {
			return b.style == SetVariable
		}
		return a.span.Start > b.span.Start
	}
	for i, c := range candidates {
		got := c.span.Text(src)
		if c.transform != nil {
			got = c.transform(got)
		}
		if c.literal && value != "" && got == value && outranks(i, best) {
			best = i
		}
	}
	if best < 0 {
		d := &Decline{Type: NotLiteral, Field: field}
		for _, c := range candidates {
			if c.style == SetVariable {
				continue
			}
			d.Candidates = append(d.Candidates, Candidate{Style: c.style, Span: c.span, Literal: c.literal})
		}
		if len(d.Candidates) == 0 {
			return Located{}, &Decline{Type: UnknownStyle, Field: field}
		}
		return Located{}, d
	}
	c := candidates[best]
	return Located{Field: field, Style: c.style, Span: c.span, Value: value}, nil
}
