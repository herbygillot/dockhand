package checkrun

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
)

// A failure whose cause a reading of its log found says that cause in one
// line under the results, with its environment where the plan has
// several; one with none keeps its last lines, and a dependency's
// failure its MacPorts line (the openssl4 session's note 6: check-773's
// sslscan read only "✗ failed at install").
func TestAFailureSaysTheCauseItsLogGave(t *testing.T) {
	t.Parallel()
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	failed := func(phase model.Phase, detail string) engine.Cell {
		return engine.Cell{Kind: engine.CellRecorded, TargetResult: model.TargetResult{Outcome: model.OutcomeFailed, Phase: phase, Detail: detail}}
	}
	cause := "Failed to build sslscan: make failed · from its log: ./sslscan.h:282:23: error: field has incomplete type 'ASN1_OCTET_STRING'"
	evidence := engine.Evidence{Plan: model.Plan{Environments: []model.Environment{tahoe}}, Targets: []engine.TargetEvidence{
		{Target: model.PlanTarget{ID: "sslscan"}, Outcomes: []engine.Cell{failed(model.PhaseInstall, cause)}},
		{Target: model.PlanTarget{ID: "socat"}, Outcomes: []engine.Cell{failed(model.PhaseDependencies, "openssl4 failed to build")}},
		{Target: model.PlanTarget{ID: "iperf3"}, Outcomes: []engine.Cell{failed(model.PhaseInstall, "Failed to build iperf3")}},
	}}
	var out bytes.Buffer
	writeFailureLines(&out, evidence)
	require.Equal(t, "sslscan failed at install: ./sslscan.h:282:23: error: field has incomplete type 'ASN1_OCTET_STRING'\n\n"+
		"socat: a dependency failed on tart macOS 26 (Tahoe) arm64: openssl4 failed to build\n\n", out.String(), "iperf3 has no log, so no last lines")

	sequoia := tahoe
	sequoia.Platform.Version = "24"
	evidence.Plan.Environments = []model.Environment{sequoia, tahoe}
	evidence.Targets = []engine.TargetEvidence{{Target: model.PlanTarget{ID: "sslscan"}, Outcomes: []engine.Cell{{Kind: engine.CellRecorded, TargetResult: model.TargetResult{Outcome: model.OutcomePassed}}, failed(model.PhaseInstall, cause)}}}
	out.Reset()
	writeFailureLines(&out, evidence)
	require.Equal(t, "sslscan failed at install on tart macOS 26 (Tahoe) arm64: ./sslscan.h:282:23: error: field has incomplete type 'ASN1_OCTET_STRING'\n\n", out.String())
}

// A check still running at --for's bound says the journal's latest
// progress, and one still queued its latest wait, with the wait that
// follows it further.
func TestAStillRunningCheckSaysWhatItsDoing(t *testing.T) {
	t.Parallel()
	bound := Bound{Said: "9m"}
	running := model.Run{Number: 42, State: model.RunRunning}
	err := stillRunning(running, &journal{latest: "building socat on macOS 26"}, bound)
	require.EqualError(t, err, "check-42 is still running after 9m: building socat on macOS 26; it continues")
	require.Equal(t, 4, term.ExitCode(err))
	queued := model.Run{Number: 42, State: model.RunQueued}
	require.EqualError(t, stillRunning(queued, &journal{waited: "waiting for a slot on tart (check-41 is using it)"}, bound),
		"check-42 is still queued after 9m, waiting for a slot on tart (check-41 is using it); it continues")

	for value, want := range map[string]string{"9": "--for 9: name a duration with its unit, as 9m or 1h", "-1m": "--for -1m: the bound is above zero", "nine": "--for nine: name a duration with its unit, as 9m or 1h"} {
		_, err := ParseBound(value)
		require.EqualError(t, err, want)
	}
	parsed, err := ParseBound("1h30m")
	require.NoError(t, err)
	require.Equal(t, Bound{For: 90 * time.Minute, Said: "1h30m"}, parsed)
}
