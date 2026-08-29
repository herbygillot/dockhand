// Package portstyle knows the styles in which Portfiles express their
// fields: the idioms — a literal version line, a go.setup argument, an
// R.setup argument — through which evaluated values appear in source. It is the
// correspondence layer between the info vocabulary and the syntax tree,
// and it produces exactly two things: a Located field whose span provably
// carries the evaluated value, or a typed Decline that says why not.
//
// "Style" here means observed, corroborated idiom — never enforcement.
// dockhand is not a formatter or linter, and nothing in this package judges
// how a Portfile ought to look; it only reports how this one does.
//
// The corroboration rule is the package's soul: a span is never trusted on
// pattern grounds alone. A style proposes candidate spans, a candidate
// counts only if its text — through the style's declared transform, when
// the PortGroup applies one — equals the value evaluation reported, and when
// several corroborate, the last in document order wins, because that is
// Tcl's own later-assignment-wins semantics. A located span is therefore
// verified by construction, and a decline states precisely which link
// failed: no known style, or no literal one.
//
// A location is one field, one span. Portfiles routinely straddle styles
// — github.setup holding a commit SHA while a version line below sets the
// real version — and corroboration resolves which span carries the field.
// What it does not do is enumerate every span a composite edit must touch:
// bumping a straddled port means moving the SHA too, and that SHA is a
// different locand, corroborated against a different evaluated value
// (github.version, a worker option). Composite edits are the planner's
// job, composed from several locations; this package answers one question
// at a time.
package portstyle
