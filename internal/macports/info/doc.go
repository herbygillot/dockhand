// Package info is the macports domain's reference vocabulary, named for what
// MacPorts itself calls this data: PortInfo. It holds the pure declarations
// every consumer of port knowledge shares — identity (who a context is),
// state (what it evaluates to), and the fetch surface (where its distfiles
// come from) — with no machinery attached: naming a port, keying a
// snapshot, comparing two of them, or scripting an answer to the fetch
// question never requires importing the evaluator. Real values come from
// macports/eval, the one producer in the tree; the types themselves are
// plain data, which is what lets plan render an added context against an
// empty Values and a test build one by hand.
package info
