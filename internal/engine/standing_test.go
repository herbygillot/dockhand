package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// One verdict of a branch's checks, which status words and serve's passing
// branches read (the command package review's concept 2).
func TestCheckStanding(t *testing.T) {
	run := func(state model.RunState) *model.Run { return &model.Run{Number: 1, State: state} }
	for want, status := range map[CheckStanding]BranchStatus{
		NotChecked:     {},
		Checking:       {Active: []model.Run{*run(model.RunRunning)}, Latest: run(model.RunPassed), Current: true},
		CheckStopped:   {Active: []model.Run{*run(model.RunRunning)}, Stopped: run(model.RunRunning)},
		CheckPassed:    {Latest: run(model.RunPassed), Current: true, Evidence: &Evidence{}},
		CheckedOlder:   {Latest: run(model.RunPassed)},
		CheckFailed:    {Latest: run(model.RunFailed), Current: true},
		CheckAttention: {Latest: run(model.RunAttention), Current: true},
	} {
		require.Equal(t, want, status.CheckStanding(), "%d", want)
	}
}
