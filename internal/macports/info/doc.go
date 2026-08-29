// Package info is the macports domain's reference vocabulary, named for what
// MacPorts itself calls this data: PortInfo. It holds the pure declarations
// every consumer of port knowledge shares — identity (who a context is) and
// state (what it evaluates to) — with no machinery attached: naming a port,
// keying a snapshot, or comparing two of them never requires importing the
// evaluator. Only macports/eval produces these values.
package info
