package cmdtest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// KeptRequest is the change request kept on the branch named, as the
// engine reads it; nil where the branch keeps none (#195).
func KeptRequest(t *testing.T, branch string) *model.ChangeRequest {
	t.Helper()
	e, err := TestSettings(t).Open(t.Context())
	require.NoError(t, err)
	defer e.Close()
	found, err := e.ResolveRecord(t.Context(), branch)
	require.NoError(t, err)
	return found.Request
}
