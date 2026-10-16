package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A live status redraws for what changes it, not for a check's progress
// (SQL-O6).
func TestWhatChangesStatus(t *testing.T) {
	for _, kind := range []EventKind{EventRunState, EventRunCancel, EventPullRequestObserved, EventReview, EventServeSubmit, EventCleanup, EventBranchState, CheckpointEvent(CheckpointTidy)} {
		require.True(t, kind.ChangesStatus(), kind)
	}
	for _, kind := range []EventKind{EventProgress, EventTargetResult, EventExecutionState, EventSessionStart, EventLeaseAcquire, EventArchiveGone} {
		require.False(t, kind.ChangesStatus(), kind)
	}
}

func TestLevelsAtLeast(t *testing.T) {
	require.Equal(t, []EventLevel{LevelInfo}, LevelInfo.AtLeast())
	require.Equal(t, []EventLevel{LevelInfo, LevelVerbose}, LevelVerbose.AtLeast())
	require.Equal(t, []EventLevel{LevelInfo, LevelVerbose, LevelDebug}, EventLevel("").AtLeast())
}
