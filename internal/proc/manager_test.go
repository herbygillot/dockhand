package proc

import (
	"context"
	"errors"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

type scriptedEngine struct {
	stages               []record.Job
	index, cycles, reads int
	problem              bool
	cycleErr             error
	onCycle              func()
}

func (e *scriptedEngine) Status(ctx context.Context, scope workflow.Scope) (workflow.Status, error) {
	e.reads++
	if err := ctx.Err(); err != nil {
		return workflow.Status{}, err
	}
	return workflow.Status{Jobs: []view.JobStatus{{Job: e.stages[e.index]}}}, nil
}
func (e *scriptedEngine) Cycle(ctx context.Context, scope workflow.Scope) (workflow.CycleResult, error) {
	e.cycles++
	if e.onCycle != nil {
		e.onCycle()
	}
	if e.cycleErr != nil {
		return workflow.CycleResult{}, e.cycleErr
	}
	if e.index < len(e.stages)-1 {
		e.index++
	}
	result := workflow.CycleResult{Advanced: []record.JobID{"job"}}
	if e.problem {
		result.Problems = []workflow.JobProblem{{JobID: "job", Detail: "temporary"}}
	}
	return result, nil
}
func stages() []record.Job {
	now := time.Now()
	return []record.Job{{ID: "job", State: record.JobQueued}, {ID: "job", State: record.JobActive}, {ID: "job", State: record.JobActive, AdmittedAt: &now}, {ID: "job", State: record.JobCompleted, AdmittedAt: &now, FinishedAt: &now}}
}
func TestAttachmentMilestonesUseRecordedProgress(t *testing.T) {
	for _, test := range []struct {
		name      string
		milestone workflow.Milestone
		cycles    int
		state     record.JobState
	}{{"admission", workflow.Admission, 2, record.JobActive}, {"completion", workflow.Completion, 3, record.JobCompleted}} {
		t.Run(test.name, func(t *testing.T) {
			engine := &scriptedEngine{stages: stages(), problem: true}
			problems := 0
			manager := Manager{Interval: time.Hour, OnCycle: func(result workflow.CycleResult) error { problems += len(result.Problems); return nil }}
			result, err := manager.Attach(t.Context(), engine, workflow.Scope{Jobs: []record.JobID{"job"}}, test.milestone, nil)
			require.NoError(t, err)
			require.Equal(t, test.cycles, engine.cycles)
			require.Equal(t, test.cycles, problems, "per-job problems must not end attachment")
			require.Equal(t, test.state, result.Jobs[0].Job.State)
		})
	}
}
func TestAttachmentStopsOnFailureWithoutAnotherSubmission(t *testing.T) {
	engine := &scriptedEngine{stages: []record.Job{{ID: "job", State: record.JobNeedsAttention}}}
	manager := Manager{}
	result, err := manager.Attach(t.Context(), engine, workflow.Scope{Jobs: []record.JobID{"job"}}, workflow.Admission, nil)
	require.NoError(t, err)
	require.Zero(t, engine.cycles)
	require.Equal(t, record.JobNeedsAttention, result.Jobs[0].Job.State)
}
func TestInterruptDetachesAndDoesNotRunAnotherCycle(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	engine := &scriptedEngine{stages: stages()}
	manager := Manager{Interval: time.Hour}
	result, err := manager.Attach(ctx, engine, workflow.Scope{Jobs: []record.JobID{"job"}}, workflow.Completion, func(workflow.Status) error { cancel(); return nil })
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, engine.cycles)
	require.Equal(t, record.JobQueued, result.Jobs[0].Job.State)
}
func TestResidentCyclesAvoidStatusHistoryAndStopOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	engine := &scriptedEngine{onCycle: cancel}
	manager := Manager{Interval: time.Hour}
	require.ErrorIs(t, manager.Run(ctx, engine, workflow.Scope{All: true}), context.Canceled)
	require.Equal(t, 1, engine.cycles)
	require.Zero(t, engine.reads)
}
func TestStateErrorsAndInvalidScopeStopAttachment(t *testing.T) {
	sentinel := errors.New("database unavailable")
	engine := &scriptedEngine{stages: stages(), cycleErr: sentinel}
	manager := Manager{Interval: time.Millisecond}
	_, err := manager.Attach(t.Context(), engine, workflow.Scope{All: true}, workflow.Completion, nil)
	require.Error(t, err)
	require.Zero(t, engine.reads)
	_, err = manager.Attach(t.Context(), engine, workflow.Scope{Jobs: []record.JobID{"job"}}, workflow.Completion, nil)
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 1, engine.cycles)
}
