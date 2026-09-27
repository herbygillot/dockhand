package ghactions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A port's result is its runners' together, and keeps each runner's part:
// failed where any failed, naming it, and passed where every runner that
// built it passed. A runner that listed the subports without it didn't
// build it, as MacPorts' workflow leaves a port off a macOS it doesn't
// support; a runner that stopped before listing, or never reached it,
// leaves no verdict yet.
func TestAPortsVerdictIsItsRunnersTogether(t *testing.T) {
	passed := map[string]*Built{"jq": {Listed: true, Installing: true, Tested: true}}
	failed := map[string]*Built{"jq": {Listed: true, Installing: true, Install: true}}
	unreached := map[string]*Built{"jq": {Listed: true}}
	elsewhere := map[string]*Built{"harbor": {Listed: true, Installing: true}}
	on := func(name, log string, built map[string]*Built) runner {
		return runner{job: RunnerJob{Name: name}, log: log, built: built, listing: true}
	}

	result, ok := verdict("jq", []runner{on("macos-14", "14.log", passed), on("macos-15", "15.log", failed)})
	require.True(t, ok)
	require.Equal(t, model.TargetResult{Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Tests: model.TestsNone, Log: "15.log", Detail: "on macos-15",
		Builders: []model.BuilderResult{
			{Builder: "macos-14", Outcome: model.OutcomePassed, Tests: model.TestsPassed, Log: "14.log"},
			{Builder: "macos-15", Outcome: model.OutcomeFailed, Phase: model.PhaseInstall, Tests: model.TestsNone, Log: "15.log"},
		}}, result)

	result, ok = verdict("jq", []runner{on("macos-14", "14.log", passed), on("macos-15", "15.log", passed)})
	require.True(t, ok)
	require.Equal(t, model.OutcomePassed, result.Outcome)
	require.Equal(t, model.TestsPassed, result.Tests)
	require.Equal(t, "14.log", result.Log)
	require.Len(t, result.Builders, 2)

	result, ok = verdict("jq", []runner{on("macos-14", "14.log", elsewhere), on("macos-15", "15.log", passed)})
	require.True(t, ok, "a runner that listed the subports without it didn't build it")
	require.Equal(t, model.OutcomePassed, result.Outcome)
	require.Equal(t, "15.log", result.Log)
	require.Equal(t, model.BuilderResult{Builder: "macos-14", Outcome: model.OutcomeNotRun}, result.Builders[0])

	_, ok = verdict("jq", []runner{on("macos-14", "14.log", passed), on("macos-15", "15.log", unreached)})
	require.False(t, ok, "a runner that listed it and never reached it leaves no verdict")
	_, ok = verdict("jq", []runner{on("macos-14", "14.log", passed), {job: RunnerJob{Name: "macos-15"}, built: map[string]*Built{}}})
	require.False(t, ok, "a runner that stopped before listing the subports leaves no verdict")
	_, ok = verdict("jq", []runner{on("macos-14", "14.log", elsewhere)})
	require.False(t, ok, "no runner built it")
	_, ok = verdict("jq", nil)
	require.False(t, ok)

	require.Equal(t, "build-macos-14.log", logName("build (macos-14)"))
	require.Equal(t, "job.log", logName("()"))
}
