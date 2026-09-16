// Package rpc exchanges framed calls with a Tcl child over its standard streams.
//
// A Session claims a shell.Proc conversation, loads its protocol and initialization
// scripts under a handshake deadline, and serializes calls. It bounds reply frames
// and retains a bounded tail of non-protocol output. Tcl errors are returned as
// CallError; broken transport or framing prevents reuse of the session.
package rpc
