// Package syntax parses Tcl source into byte-addressed syntax without evaluating
// it.
//
// Scripts, commands, words, and substitutions retain spans into the original
// source. Explicit script and list views interpret braced content when the caller
// knows its role; visitors let callers control descent. Diagnostics and literal
// extraction support source-preserving edits rather than replacing a Tcl runtime.
package syntax
