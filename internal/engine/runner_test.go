package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// scriptedProvider builds by a script of outcomes: each attempt fails
// with infrastructure trouble while failures remain, then records the
// outcomes it was given for the targets it was asked for.
type scriptedProvider struct {
	mu       sync.Mutex
	outcomes map[model.TargetID]model.Outcome
	failures int
	// partial records the first target before a failing attempt stops.
	partial bool
	// wait holds the build until its context ends.
	wait bool
	jobs []Job
}

func (p *scriptedProvider) Name() string { return "command" }

func (p *scriptedProvider) Execute(ctx context.Context, job Job, build Build) error {
	p.mu.Lock()
	p.jobs = append(p.jobs, job)
	failing := p.failures > 0
	p.failures--
	p.mu.Unlock()
	if failing && !p.partial {
		return errors.New("the VM did not start")
	}
	for i, target := range job.Targets {
		if p.wait {
			<-ctx.Done()
			return ctx.Err()
		}
		if _, blocked := build.Blocked(target.ID); blocked {
			if err := build.Record(model.TargetResult{Target: target.ID, Outcome: model.OutcomeBlocked, Tests: model.TestsNone}); err != nil {
				return err
			}
			continue
		}
		outcome, ok := p.outcomes[target.ID]
		if !ok {
			outcome = model.OutcomePassed
		}
		result := model.TargetResult{Target: target.ID, Outcome: outcome, Tests: model.TestsNone}
		if outcome == model.OutcomeFailed {
			result.Phase = model.PhaseInstall
		}
		if err := build.Record(result); err != nil {
			return err
		}
		if failing && i == 0 {
			return errors.New("the VM stopped answering")
		}
	}
	return nil
}

func session(t *testing.T, e *Engine) *coord.Session {
	t.Helper()
	c := &coord.Coordinator{Store: e.Store, Repository: e.Repository}
	s, err := c.Start(t.Context(), model.SessionForeground, "test")
	require.NoError(t, err)
	return s
}

// queuedHarborRun plans and queues a check of the harbor branch.
func queuedHarborRun(t *testing.T, e *Engine, environments ...model.Environment) model.Run {
	t.Helper()
	revision := harborBranch(t, e)
	e.PortReader = harborPorts()
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: environments})
	require.NoError(t, err)
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		branch, err = r.Branch(revision.Branch)
		return err
	}))
	run, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	return run
}

func outcomes(t *testing.T, e *Engine, run model.Run) map[string]model.Outcome {
	t.Helper()
	found := map[string]model.Outcome{}
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		plan, err := r.Plan(run.Plan)
		require.NoError(t, err)
		evidence, err := runEvidence(r, run, plan)
		require.NoError(t, err)
		for _, target := range evidence.Targets {
			for i, result := range target.Outcomes {
				found[string(target.Target.ID)+"@"+plan.Environments[i].Platform.Architecture] = result.Outcome
			}
		}
		return nil
	}))
	return found
}

func TestARunPassesAndIsEvidence(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	provider := &scriptedProvider{}
	e.Providers = map[string]Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm, tahoeX86)
	require.Equal(t, "check-1", queued.Name())

	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	require.Len(t, provider.jobs, 2, "one execution per environment")
	var armTargets []model.TargetID
	for _, target := range provider.jobs[0].Targets {
		armTargets = append(armTargets, target.ID)
	}
	require.NotContains(t, armTargets, model.TargetID("harbor-viewer-legacy"), "excluded where supported_archs rules it out")
	require.NotEmpty(t, provider.jobs[0].Commit)
	require.Equal(t, model.OutcomeNotRun, outcomes(t, e, run)["harbor-viewer-legacy@arm64"])
	require.Equal(t, model.OutcomePassed, outcomes(t, e, run)["harbor-viewer-legacy@x86_64"])

	var revision model.Revision
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		revision, err = r.Revision(run.Revision)
		return err
	}))
	evidence, found, err := e.EvidenceFor(t.Context(), run.Branch, revision.Source.Tree)
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, evidence.Failed(), "an excluded target is not required to pass")
}

func TestAFailedDependencyBlocksAndInfrastructureIsRetried(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	provider := &scriptedProvider{outcomes: map[model.TargetID]model.Outcome{"harbor-cli": model.OutcomeFailed}, failures: 1, partial: true}
	e.Providers = map[string]Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)

	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunFailed, run.State)
	require.Len(t, provider.jobs, 2, "the stopped VM is tried again")
	require.Len(t, provider.jobs[1].Targets, len(provider.jobs[0].Targets)-1, "libharbor's verdict from the first attempt stands")
	got := outcomes(t, e, run)
	require.Equal(t, model.OutcomePassed, got["libharbor@arm64"])
	require.Equal(t, model.OutcomeFailed, got["harbor-cli@arm64"])
	require.Equal(t, model.OutcomeBlocked, got["harbor-viewer@arm64"], "blocked, not failed")
	require.Equal(t, "harbor-cli, harbor-viewer did not pass", run.Detail)
}

func TestADependencyFailureBlocksWithoutARetry(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	provider := &scriptedProvider{outcomes: map[model.TargetID]model.Outcome{"libharbor": model.OutcomeFailed}, failures: 1, partial: true}
	e.Providers = map[string]Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Len(t, provider.jobs, 1, "everything after the failure is blocked, so nothing is left to try")
	got := outcomes(t, e, run)
	require.Equal(t, model.OutcomeBlocked, got["harbor-cli@arm64"])
	require.Equal(t, model.OutcomeBlocked, got["harbor-viewer@arm64"])
}

func TestRepeatedInfrastructureTroubleNeedsAttention(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	provider := &scriptedProvider{failures: 99}
	e.Providers = map[string]Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm, model.Environment{Provider: "tart"})

	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunAttention, run.State)
	require.Len(t, provider.jobs, model.MaxAttempts)
	require.Contains(t, run.Detail, "no provider \"tart\" is set up here")
	require.Contains(t, run.Detail, "failed 3 times")
}

func TestACancelIsAppliedByWhoeverHoldsTheRun(t *testing.T) {
	f := setup(t)
	f.options.Poll = 10 * time.Millisecond
	e := f.open(t)
	provider := &scriptedProvider{wait: true}
	e.Providers = map[string]Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	runner, canceller := session(t, e), session(t, e)

	done := make(chan model.Run)
	go func() {
		run, err := e.Drive(context.Background(), runner, queued.ID)
		require.NoError(t, err)
		done <- run
	}()
	require.Eventually(t, func() bool {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		return len(provider.jobs) == 1
	}, 5*time.Second, 10*time.Millisecond)
	run, err := e.RequestCancel(t.Context(), canceller, queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunRunning, run.State, "the holder applies it")
	run = <-done
	require.Equal(t, model.RunCanceled, run.State)

	// A queued run is canceled at once.
	second, err := e.Enqueue(t.Context(), model.Branch{ID: run.Branch}, mustPlan(t, e, run), model.OriginPerson)
	require.NoError(t, err)
	canceled, err := e.RequestCancel(t.Context(), canceller, second.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunCanceled, canceled.State)
}

func mustPlan(t *testing.T, e *Engine, run model.Run) model.Plan {
	t.Helper()
	var plan model.Plan
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		plan, err = r.Plan(run.Plan)
		return err
	}))
	plan.ID = model.PlanID(store.NewID("plan"))
	return plan
}
