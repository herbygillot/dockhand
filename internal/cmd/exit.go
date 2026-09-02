package cmd

import (
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/verify"
)

// UsageError marks a failure as the invocation being wrong — the remedy
// is rereading --help, not fixing the machine or the tree. Flag-parse
// errors are wrapped into it by Root's FlagErrorFunc; argument
// validation wraps its own.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

// ExitCode: the invocation band — the remedy is rereading --help.
func (e *UsageError) ExitCode() int { return exitcode.Usage }

// usagef builds a UsageError from a format string.
func usagef(format string, a ...any) error {
	return &UsageError{Err: fmt.Errorf(format, a...)}
}

// ExitCode maps an error from the command tree to a process exit code.
// Typed errors own their band (exitcode.Coder, checked first); the
// table below covers only sentinels, which cannot carry a method.
func ExitCode(err error) int {
	if err == nil {
		return exitcode.OK
	}
	var coder exitcode.Coder
	if errors.As(err, &coder) {
		return coder.ExitCode()
	}
	switch {
	case errors.Is(err, verify.ErrNoEnvironment),
		errors.Is(err, verify.ErrUnsupported),
		errors.Is(err, prefix.ErrNotInstalled),
		errors.Is(err, eval.ErrStartup),
		errors.Is(err, eval.ErrRootRefused),
		errors.Is(err, portfetch.ErrRootRefused):
		return exitcode.Environment
	case errors.Is(err, tree.ErrNotPortsTree),
		errors.Is(err, tree.ErrPortNotFound),
		errors.Is(err, git.ErrNotARepo):
		// A tree that is not a git checkout is a fact about the tree:
		// the remedy is a different checkout or --in-place, never
		// fixing the machine.
		return exitcode.Tree
	default:
		return exitcode.Failure
	}
}
