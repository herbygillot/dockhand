package macports

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnansweredCallIsLosslessButItsDisplayIsBounded(t *testing.T) {
	call := strings.Repeat("x", 600) + "TAIL-MARKER"
	err := &UnansweredError{Call: call}
	require.Equal(t, call, err.Call)
	require.NotContains(t, err.Error(), "TAIL-MARKER")
	require.Len(t, HostCallText(call), 203)
}
