package portedit

import (
	"fmt"
	"io"
	"testing"

	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/stretchr/testify/require"
)

func TestAWorkerFailureIsNotAnAssessmentVerdict(t *testing.T) {
	for _, cause := range []error{fmt.Errorf("%w: %w", rpc.ErrBrokenSession, io.EOF), rpc.ErrHandshake, rpc.ErrNoAnswer} {
		got := Problem("evaluation", fmt.Errorf("evaluating fixture: %w", cause))
		require.Equal(t, "evaluation-unavailable", got.Code)
		require.Equal(t, Unknown, got.Status)
	}
	got := Problem("evaluation", rpc.CallError{Msg: "the Portfile needs an answer"})
	require.Equal(t, "check-inconclusive", got.Code, "a Tcl verdict is not transport failure")
}
