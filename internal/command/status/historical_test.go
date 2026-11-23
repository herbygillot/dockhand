package status

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

func TestStatusLabelsResultsForEarlierFilesAndNamesPortsAddedSince(t *testing.T) {
	t.Parallel()
	run := model.Run{ID: "run_182", Number: 17, State: model.RunPassed}
	macOS := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	status := engine.BranchStatus{
		Branch:         model.Branch{Name: "dockhand/ports-update", State: model.BranchOpen},
		Latest:         &run,
		LatestRevision: &model.Revision{Kind: model.RevisionSnapshot, Snapshot: 1},
		Scope:          engine.Scope{Ports: []string{"devel/pyinfra", "python/py-cyclopts", "sysutils/unison"}},
		HistoricalResults: []engine.HistoricalPortResult{
			{Port: "pyinfra", State: engine.HistoricalPortChanged},
			{Port: "py-cyclopts", State: engine.HistoricalPortNotInCheck},
			{Port: "unison", State: engine.HistoricalPortUnchanged},
		},
		Evidence: &engine.Evidence{
			Run:  run,
			Plan: model.Plan{Environments: []model.Environment{macOS}},
			Targets: []engine.TargetEvidence{
				{
					Target:   model.PlanTarget{ID: "pyinfra", Target: model.Target{Name: "pyinfra"}},
					Passed:   true,
					Outcomes: []engine.Cell{{Kind: engine.CellRecorded, Environment: macOS, TargetResult: model.TargetResult{Target: "pyinfra", Outcome: model.OutcomePassed}}},
				},
				{
					Target:   model.PlanTarget{ID: "unison", Target: model.Target{Name: "unison"}},
					Passed:   true,
					Outcomes: []engine.Cell{{Kind: engine.CellRecorded, Environment: macOS, TargetResult: model.TargetResult{Target: "unison", Outcome: model.OutcomePassed}}},
				},
			},
		},
	}
	var out bytes.Buffer
	showBranch(&out, config.Places{}, status, engine.StatusReport{})

	require.Contains(t, out.String(), "check-17: results for earlier files\n")
	require.Contains(t, out.String(), "pyinfra (files changed since check-17)  tart macOS 26 (Tahoe) arm64 ✓\n")
	require.Regexp(t, `unison +tart macOS 26 \(Tahoe\) arm64 ✓`, out.String())
	require.NotContains(t, out.String(), "unison (files changed since check-17)")
	require.Contains(t, out.String(), "py-cyclopts  not checked by check-17\n")

	status.HistoricalResults = nil
	out.Reset()
	showBranch(&out, config.Places{}, status, engine.StatusReport{})
	require.Contains(t, out.String(), "check-17: results for earlier files\n")
	require.NotContains(t, out.String(), "files changed since check-17")
	require.NotContains(t, out.String(), "not checked by check-17")
}
