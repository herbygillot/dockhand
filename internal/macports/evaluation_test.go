package macports

import (
	"context"
	"errors"
	"fmt"
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
