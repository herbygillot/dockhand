// Package eval evaluates Portfiles through MacPorts' own interpreter and
// returns their metadata as values.
//
// This is the read half of the design's central asymmetry: values come from
// evaluation through port-tclsh — never from reading the Portfile's text —
// because a Portfile's metadata is frequently computed rather than literal.
// Locations come from the syntax package; the two must never be confused,
// and nothing in this package handles spans.
//
// The evaluator is an rpc.Session over port-tclsh with a MacPorts shim
// registered as its init script. Replies are Tcl dicts built by Tcl's own
// list machinery and decoded here with the syntax package's list lens: the
// oracle serializes in the oracle's native format.
package eval
