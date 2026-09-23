package sqlite

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestPreparingActionsSpellsTheRecordsRule(t *testing.T) {
	t.Parallel()
	for _, action := range []record.Action{record.Bump, record.BumpRevision, record.RefreshChecksums, record.Amend, record.Rebase, record.Verify, record.Publish} {
		require.Equal(t, action.Prepares(), strings.Contains(preparingActions, "'"+string(action)+"'"), string(action))
	}
}
