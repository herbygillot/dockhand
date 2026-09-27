package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// A result reads under the policy of the check that built it. One that
// came from an earlier check whose policy differs from the evidence's own
// names that check, so an advisory failure never reads as required.
func TestAResultReadsUnderItsOwnChecksPolicy(t *testing.T) {
	required := model.Run{ID: "run_new", Number: 4}
	advisory := model.Run{ID: "run_old", Number: 3}
	evidence := Evidence{
		Run:     required,
		Plan:    model.Plan{Tests: model.TestsRequired, Environments: []model.Environment{{Provider: "command"}}},
		Earlier: []model.Run{advisory},
		Executions: map[model.ExecutionID]model.GuestExecution{
			"ex_new": {ID: "ex_new", Run: required.ID},
			"ex_old": {ID: "ex_old", Run: advisory.ID},
		},
		policies: map[model.RunID]model.TestPolicy{required.ID: model.TestsRequired, advisory.ID: model.TestsDeclared},
	}
	old := TargetEvidence{Target: model.PlanTarget{ID: "libharbor"}, Outcomes: []model.TargetResult{{Execution: "ex_old", Outcome: model.OutcomePassed, Tests: model.TestsFailed}}}
	require.Equal(t, "✓ build passed; tests failed (advisory, check-3)", evidence.Words(old, 0, false))
	evidence.Plan.Tests = model.TestsDeclared
	require.Equal(t, "✓ build passed; tests failed (advisory)", evidence.Words(old, 0, false), "the same policy needs no check named")
}

// advisoryHarbor builds every target, and libharbor's tests fail when the
// check's policy lets them: advisory in one check, required in another.
type advisoryHarbor struct{}

func (*advisoryHarbor) Name() string { return "command" }
func (*advisoryHarbor) Execute(_ context.Context, job buildenv.Job, build buildenv.Build) error {
	for _, target := range job.Targets {
		tests := model.TestsPassed
		if target.ID == "libharbor" {
			tests = model.TestsFailed
		}
		if err := build.Record(model.TargetResult{Target: target.ID, Outcome: model.OutcomePassed, Tests: tests}); err != nil {
			return err
		}
	}
	return nil
}

// A result keeps the policy of the check that built it (the person's
// decision on D1, 2026-09-27). After a full advisory check, a check of jq
// alone with --tests required leaves libharbor's advisory result standing:
// the submission isn't blocked by it, and the pull request says under
// which check's policy its tests failed.
func TestAnEarlierAdvisoryResultKeepsItsPolicy(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	e.Providers = map[string]buildenv.Provider{"command": &advisoryHarbor{}}
	checkHead(t, e, branch)
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch, Mode: CaptureHead})
	require.NoError(t, err)
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}, Only: []string{"jq"}, Tests: model.TestsRequired})
	require.NoError(t, err)
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)

	submission, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.Empty(t, submission.Blocking)
	require.Contains(t, submission.Body, "| libharbor | ✓ build passed; tests failed (advisory, check-1) |")
}
