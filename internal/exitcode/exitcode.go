// Package exitcode is the process-exit contract: an exit status
// answers "whose problem is this" — the invocation, the machine, the
// tree, or the operation. Documented in docs/cli.md; once dockhand
// ships these are a contract for scripts branching on $?.
//
// A typed error that knows its own band implements Coder where the
// error is defined, so the mapping cannot be forgotten in a table two
// packages away — the forget-me trap every new error type used to
// walk into.
package exitcode

const (
	OK          = 0 // success; for sweeps, success even with a tail of declines
	Failure     = 1 // the operation itself failed
	Usage       = 2 // bad flag, unknown command, invalid arguments
	Environment = 3 // the machine: MacPorts missing, tclsh broken, running as root
	Tree        = 4 // the ports tree: not a tree, port not found
	Declined    = 5 // a point intent declined to produce a plan
	Verify      = 6 // verification ran and the port does not build
)

// Coder is implemented by errors that own their exit band.
type Coder interface {
	ExitCode() int
}
