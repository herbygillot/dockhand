package actions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

func TestAPortsVerdictNeedsEveryRunner(t *testing.T) {
	passed := map[string]*Built{"jq": {Listed: true, Installing: true, Tested: true}}
	failed := map[string]*Built{"jq": {Listed: true, Installing: true, Install: true}}
	unreached := map[string]*Built{"jq": {Listed: true}}

	result, ok := verdict("jq", []runner{{log: "14.log", built: passed}, {log: "15.log", built: failed}})
	require.True(t, ok)
	require.Equal(t, model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Tests: model.TestsNone, Log: "15.log"}, result)

	result, ok = verdict("jq", []runner{{log: "14.log", built: passed}, {log: "15.log", built: passed}})
	require.True(t, ok)
	require.Equal(t, model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsPassed, Log: "14.log"}, result)

	_, ok = verdict("jq", []runner{{built: passed}, {built: unreached}})
	require.False(t, ok, "a runner that never reached it leaves no verdict")
	_, ok = verdict("jq", []runner{{built: passed}, {built: map[string]*Built{}}})
	require.False(t, ok, "a runner that never listed it leaves no verdict")
	_, ok = verdict("jq", nil)
	require.False(t, ok)

	require.Equal(t, "build-macos-14.log", logName("build (macos-14)"))
	require.Equal(t, "job.log", logName("()"))
}
