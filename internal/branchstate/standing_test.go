package branchstate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
)

// One verdict of a branch's checks, which status words and serve's passing
// branches read (the command package review's concept 2).
func TestStanding(t *testing.T) {
	run := func(state model.RunState) *model.Run { return &model.Run{Number: 1, State: state} }
	for want, status := range map[Standing]Check{
		NotChecked:     {},
		Checking:       {Active: []model.Run{*run(model.RunRunning)}, Latest: run(model.RunPassed), Current: true},
		CheckStopped:   {Active: []model.Run{*run(model.RunRunning)}, Stopped: run(model.RunRunning)},
		CheckPassed:    {Latest: run(model.RunPassed), Current: true, Evidence: &evidence.Evidence{}},
		CheckedOlder:   {Latest: run(model.RunPassed)},
		CheckFailed:    {Latest: run(model.RunFailed), Current: true},
		CheckAttention: {Latest: run(model.RunAttention), Current: true},
	} {
		require.Equal(t, want, status.Standing(), "%d", want)
	}
}

// A baseline running beside a passed check, or a run asked to stop, isn't
// the branch's check: its standing is the passed check's, as ActiveCheck
// and the queue judge (Run.BranchCheck), so serve's PassingBranches no
// longer waits on a baseline (the architecture review of 5897a82, R3-2).
func TestABaselineOrAStoppingRunLeavesTheBranchsStanding(t *testing.T) {
	passed := &model.Run{Number: 1, State: model.RunPassed}
	baseline := model.Run{Number: 2, State: model.RunRunning, BaselineOf: "run-1"}
	stopping := model.Run{Number: 3, State: model.RunRunning, CancelRequested: new(time.Time)}
	check := model.Run{Number: 4, State: model.RunQueued}
	for name, tc := range map[string]struct {
		active []model.Run
		want   Standing
		run    int
	}{
		"baseline":           {[]model.Run{baseline}, CheckPassed, 0},
		"asked to stop":      {[]model.Run{stopping}, CheckPassed, 0},
		"a check beside one": {[]model.Run{check, baseline}, Checking, 4},
	} {
		status := Check{Active: tc.active, Latest: passed, Current: true, Evidence: &evidence.Evidence{}}
		require.Equal(t, tc.want, status.Standing(), name)
		if tc.run == 0 {
			require.Nil(t, status.Checking(), name)
		} else {
			require.Equal(t, tc.run, status.Checking().Number, name)
		}
	}
}

// Each standing has a name of its own, as status's JSON gives it (R3-34).
func TestEachStandingHasAName(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for s := NotChecked; s <= CheckAttention; s++ {
		require.NotEmpty(t, s.String())
		require.False(t, seen[s.String()], s.String())
		seen[s.String()] = true
	}
	require.Equal(t, "passed", CheckPassed.String())
}
