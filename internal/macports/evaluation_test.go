package macports

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/stretchr/testify/require"
)

func TestEvaluationUnavailableUsesTypedInfrastructureErrors(t *testing.T) {
	for _, err := range []error{ErrStartup, rpc.ErrBrokenSession, rpc.ErrHandshake, rpc.ErrNoAnswer, context.Canceled, context.DeadlineExceeded} {
		require.True(t, IsEvaluationUnavailable(fmt.Errorf("wrapped: %w", err)))
	}
	for _, err := range []error{nil, ErrRefused, ErrUnanswered, errors.New("rpc: no handshake")} {
		require.False(t, IsEvaluationUnavailable(err))
	}
}

// dockhand's own evaluator exiting is said as that, with its last words,
// and never as a finding about the port; it stays an unavailable
// evaluation for the assessments (#253).
func TestAnEvaluatorThatExitedIsSaidPlainly(t *testing.T) {
	t.Parallel()
	broken := fmt.Errorf("rpc: read: %w", &rpc.BrokenError{Cause: io.EOF, Stderr: "Segmentation fault: 11"})
	err := Exited("sysutils/skim/Portfile", broken)
	require.EqualError(t, err, `dockhand's Portfile evaluator exited while evaluating sysutils/skim/Portfile (its last words: "Segmentation fault: 11"): that is no finding about the port, and running the command again is safe`)
	require.True(t, IsEvaluationUnavailable(err))
	require.ErrorIs(t, err, rpc.ErrBrokenSession)
	other := errors.New("macports: something else")
	require.Equal(t, other, Exited("sysutils/skim/Portfile", other), "any other error is left as it is")
}
