package evidence

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A port's builds are found in their logs from the install step, which
// builds it, to the next step of any target sharing the log, one span for
// each environment that recorded a log; a subport is its own port.
func TestAPortsBuildsAreFoundInTheirLogs(t *testing.T) {
	recorded := func(log string, steps ...model.LogStep) Cell {
		return Cell{Kind: CellRecorded, TargetResult: model.TargetResult{Log: log, Steps: steps}}
	}
	ev := Evidence{Targets: []TargetEvidence{
		{Target: model.PlanTarget{ID: "omnidiff", Target: model.Target{Name: "omnidiff"}}, Outcomes: []Cell{
			recorded("/logs/a.log", model.LogStep{Name: model.StepDependencies, Line: 3}, model.LogStep{Name: model.StepFetch, Line: 40}, model.LogStep{Name: model.StepInstall, Line: 60}, model.LogStep{Name: model.StepTest, Line: 900}),
			recorded("/logs/b.log", model.LogStep{Name: model.StepDependencies, Line: 1}, model.LogStep{Name: model.StepFetch, Line: 10}),
			{Kind: CellNotRun},
		}},
		{Target: model.PlanTarget{ID: "other", Target: model.Target{Name: "other"}}, Outcomes: []Cell{
			recorded("/logs/a.log", model.LogStep{Name: model.StepDeactivate, Line: 950}),
			recorded("/logs/b.log", model.LogStep{Name: model.StepDeactivate, Line: 500}),
		}},
		{Target: model.PlanTarget{ID: "py-x", Target: model.Target{Name: "py-x", Subport: "py313-x"}}, Outcomes: []Cell{recorded("/logs/c.log")}},
	}}
	require.Equal(t, []LogSpan{{Log: "/logs/a.log", From: 60, To: 900}, {Log: "/logs/b.log", From: 10, To: 500}}, ev.BuildSpans("omnidiff"))
	require.Equal(t, []LogSpan{{Log: "/logs/c.log", From: 1}}, ev.BuildSpans("py313-x"))
	require.Empty(t, ev.BuildSpans("py-x"))
}
