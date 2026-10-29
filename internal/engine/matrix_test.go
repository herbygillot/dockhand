package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// matrixProvider is a provider whose one run builds every environment it's
// given, as GitHub's workflow does, its calls scripted.
type matrixProvider struct {
	jobs    []buildenv.Job
	execute func(call int, job buildenv.Job) error
}

func (p *matrixProvider) Name() string { return "command" }
func (p *matrixProvider) Matrix() bool { return true }
func (p *matrixProvider) Execute(_ context.Context, job buildenv.Job, _ buildenv.Build) error {
	p.jobs = append(p.jobs, job)
	return p.execute(len(p.jobs), job)
}

// passes records each of a cell's targets as passed.
func passes(t *testing.T, cell buildenv.MatrixCell) {
	t.Helper()
	for _, target := range cell.Targets {
		require.NoError(t, cell.Build.Record(buildenv.Reported(model.TargetResult{Target: target.ID, Outcome: model.OutcomePassed, Tests: model.TestsNone})))
	}
}

// A matrix provider's environments are built in one run, each recorded
// through its own execution; a retry is one run of the environments still
// without a verdict, never one per environment, so GitHub's workflow is run
// again once (the architecture thread, 2026-10-29).
func TestAMatrixRunsItsEnvironmentsTogetherAndRetriesOnce(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &matrixProvider{execute: func(call int, job buildenv.Job) error {
		switch call {
		case 1:
			passes(t, job.Matrix[0])
			return buildenv.Infrastructure("a runner went away")
		default:
			for _, cell := range job.Matrix {
				passes(t, cell)
			}
			return nil
		}
	}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm, tahoeX86)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	require.Len(t, provider.jobs, 2, "one run, then one retry")
	require.Len(t, provider.jobs[0].Matrix, 2)
	require.Len(t, provider.jobs[1].Matrix, 1, "the environment with every verdict sits the retry out")
	require.Equal(t, tahoeX86, provider.jobs[1].Matrix[0].Environment)
}

// An environment whose build never ran, as a GitHub job no runner took, is
// left unrecorded with why, and spends no attempt; the others are recorded.
// The evidence says it wasn't built, with why, never that it passed.
func TestAMatrixEnvironmentThatNeverRanIsUnbuilt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	provider := &matrixProvider{execute: func(_ int, job buildenv.Job) error {
		passes(t, job.Matrix[0])
		return &buildenv.Unrun{Cells: map[string]string{buildenv.EnvironmentKey(job.Matrix[1].Environment): "the job never ran"}}
	}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm, tahoeX86)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.NotEqual(t, model.RunPassed, run.State)
	require.Len(t, provider.jobs, 1, "no attempt is spent on it")
	found := outcomes(t, e, run)
	require.Equal(t, model.OutcomePassed, found["harbor-viewer@arm64"])
	require.Equal(t, model.OutcomeNotRun, found["harbor-viewer@x86_64"])
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		evidence, err := runEvidence(r, run)
		require.NoError(t, err)
		for _, target := range evidence.Targets {
			if target.Target.ID == "harbor-viewer" {
				require.Equal(t, "the job never ran", target.Outcomes[1].Unrun)
				require.False(t, target.Passed)
			}
		}
		return nil
	}))
}
