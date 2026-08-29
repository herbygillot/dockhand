// Package syntax parses Tcl source into a concrete syntax tree with byte spans.
//
// The tree has exactly four levels, because Tcl has no higher grammar —
// control flow and declarations are commands, not syntax:
//
//	Script   → Items (Commands and Comments)
//	Command  → Words
//	Word     → Segments (Literal | VarSub | CmdSub | Braced | Quoted)
//	Segment  → spans; CmdSub recurses as a Script
//
// Every node carries the half-open byte span [Start, End) it occupies in the
// original source. The parser never copies text: values are read through
// spans, and the concatenation invariant — walking a tree reproduces the
// input byte-for-byte — is what the tests enforce.
//
// Brace bodies are opaque. Which braced word holds a script and which holds
// a list is decided by the command consuming it, never by grammar, so the
// tree stores every brace body as a raw span and consumers reinterpret it
// through a lens: Braced.ScriptLens re-parses the body as commands,
// Braced.ListLens splits it as a Tcl list. There is deliberately no expr
// lens.
//
// This package implements the lexical rules of the Tcl Dodekalogue (see
// docs/tcl-dodekalogue.md) and nothing above them. It never evaluates,
// substitutes, or interprets: values come from evaluation through
// port-tclsh, locations come from here, and the two must never be confused.
//
// Malformed input does not abort parsing. Parse never returns nil: every
// input yields a tree that tiles it, with Errors recorded where the source
// violates the Dodekalogue itself (unterminated braces, quotes, brackets) —
// each Error kind corresponds to a rejection tclsh itself would issue.
// Callers that need certainty decline when the error list is non-empty.
//
// Note the converse does not hold: a clean parse means lexically
// well-formed, not meaningful. Tcl's lexical rules reject almost nothing —
// JSON, C source, and prose all parse without issues as nonsense commands.
// Whether input is actually a Portfile is answered above this package: by
// recognizers finding nothing to recognize, and by evaluation through
// port-tclsh rejecting it semantically.
package syntax
