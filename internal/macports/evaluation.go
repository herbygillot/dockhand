package macports

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/tcl/rpc"
)

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
