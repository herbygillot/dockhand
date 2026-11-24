package macports

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/tcl/rpc"
)

// EvaluatorExited is dockhand's own Portfile evaluator exiting while it
// evaluated a Portfile: no finding about the port (#253).
type EvaluatorExited struct {
	Portfile, Stderr string
	Err              error
}

func (e *EvaluatorExited) Error() string {
	last := ""
	if e.Stderr != "" {
		last = fmt.Sprintf(" (its last words: %q)", e.Stderr)
	}
	return fmt.Sprintf("dockhand's Portfile evaluator exited while evaluating %s%s: that is no finding about the port, and running the command again is safe", e.Portfile, last)
}

func (e *EvaluatorExited) Unwrap() error { return e.Err }

// Exited says a broken evaluation session as EvaluatorExited, and returns
// any other error as it is.
func Exited(portfile string, err error) error {
	var broken *rpc.BrokenError
	if errors.As(err, &broken) {
		return &EvaluatorExited{Portfile: portfile, Stderr: broken.Stderr, Err: err}
	}
	return err
}

// IsEvaluationUnavailable distinguishes infrastructure and cancellation from a
// semantic evaluation result. Both assessments and private experiments use it.
func IsEvaluationUnavailable(err error) bool {
	for _, cause := range []error{ErrStartup, rpc.ErrBrokenSession, rpc.ErrHandshake, rpc.ErrNoAnswer, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, cause) {
			return true
		}
	}
	return false
}
