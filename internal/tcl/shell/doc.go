// Package shell manages Tcl child processes and their bounded output.
//
// Start creates a context-bound process with configurable arguments, directory,
// and environment. Proc exposes its streams, exit state, and a stderr tail, with
// bounded shutdown and protection against multiple conversation owners. The rpc
// subpackage owns the framing and interpretation of calls over those streams.
package shell
