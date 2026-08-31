package cmd

import (
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
)

// Process exit codes. An exit status answers "whose problem is this":
// the invocation, the machine, the tree, or the operation. Documented
// in docs/cli.md; once dockhand ships these are a contract for scripts
// branching on $?.
const (
	ExitOK          = 0 // success; for sweeps, success even with a tail of declines
	ExitFailure     = 1 // the operation itself failed
	ExitUsage       = 2 // bad flag, unknown command, invalid arguments
	ExitEnvironment = 3 // the machine: MacPorts missing, tclsh broken, running as root
	ExitTree        = 4 // the ports tree: not a tree, port not found
	ExitDeclined    = 5 // reserved: a point intent declined to produce a plan
)

// UsageError marks a failure as the invocation being wrong — the remedy
// is rereading --help, not fixing the machine or the tree. Flag-parse
// errors are wrapped into it by Root's FlagErrorFunc; argument
// validation wraps its own.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

// usagef builds a UsageError from a format string.
func usagef(format string, a ...any) error {
	return &UsageError{Err: fmt.Errorf(format, a...)}
}

// ExitCode maps an error from the command tree to a process exit code.
func ExitCode(err error) int {
	var usage *UsageError
	var styleDecline *portstyle.Decline
	var intentDecline *plan.Decline
	switch {
	case err == nil:
		return ExitOK
	case errors.As(err, &styleDecline),
		errors.As(err, &intentDecline):
		return ExitDeclined
	case errors.As(err, &usage):
		return ExitUsage
	case errors.Is(err, tree.ErrNotPortsTree),
		errors.Is(err, tree.ErrPortNotFound):
		return ExitTree
	case errors.Is(err, prefix.ErrNotInstalled),
		errors.Is(err, eval.ErrStartup),
		errors.Is(err, eval.ErrRootRefused),
		errors.Is(err, portfetch.ErrRootRefused):
		return ExitEnvironment
	default:
		return ExitFailure
	}
}
