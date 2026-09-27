package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
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
	// active are reported as the ports active as each target built.
	active []model.ActivePort
	jobs   []buildenv.Job
}

func (p *scriptedProvider) Name() string { return "command" }

func (p *scriptedProvider) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) error {
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
		if p.active != nil {
			build.Consumed(target.ID, p.active)
			result.Archive = "sha256:" + string(target.ID)
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
	e.Providers = map[string]buildenv.Provider{"command": provider}
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

// What a provider saw active as a target built is kept with its result,
// each directory by the revision's tree, and the environment by its
// identity as the execution began (decision 28).
func TestAResultKeepsWhatItsBuildRead(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	tools := model.ActivePort{Name: "harbor-tools", Spec: "@1_0", Directory: "graphics/harbor-tools", Archive: "sha256:aa"}
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{tools}}, identity: "origin a"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	checked, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, checked.State, checked.Detail)

	var revision model.Revision
	var results []model.TargetResult
	var inputs model.TargetInputs
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		if revision, err = r.Revision(checked.Revision); err != nil {
			return err
		}
		executions, err := r.Executions(checked.ID)
		if err != nil {
			return err
		}
		if results, err = r.Results(executions[0].ID); err != nil {
			return err
		}
		inputs, err = r.Inputs(results[0].Inputs)
		return err
	}))
	for _, result := range results {
		require.NotEmpty(t, result.Inputs, "%s's build read something", result.Target)
		require.Equal(t, "sha256:"+string(result.Target), result.Archive)
	}
	tree := func(path string) model.ObjectID {
		return model.ObjectID(run(t, e.Repo.Root, "rev-parse", string(revision.Source.Tree)+":"+path))
	}
	require.Equal(t, "origin a", inputs.Environment)
	require.NotEmpty(t, inputs.Directory)
	require.Equal(t, tree(inputs.Directory), inputs.Tree, "the target's directory by the revision's tree")
	tools.Tree = tree("graphics/harbor-tools")
	require.Equal(t, []model.ActivePort{tools}, inputs.Active, "and each active port's")
	require.Equal(t, tree("_resources"), inputs.Resources)
	require.True(t, inputs.Complete())
}

func TestAFailedDependencyBlocksAndInfrastructureIsRetried(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	provider := &scriptedProvider{outcomes: map[model.TargetID]model.Outcome{"harbor-cli": model.OutcomeFailed}, failures: 1, partial: true}
	e.Providers = map[string]buildenv.Provider{"command": provider}
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
	e.Providers = map[string]buildenv.Provider{"command": provider}
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
	e.Providers = map[string]buildenv.Provider{"command": provider}
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
	e.Providers = map[string]buildenv.Provider{"command": provider}
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

func TestAStoppedServeLeavesTheRunForTheNext(t *testing.T) {
	f := setup(t)
	f.options.Poll = 10 * time.Millisecond
	e := f.open(t)
	provider := &scriptedProvider{wait: true}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	first := session(t, e)

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan model.Run)
	go func() {
		run, err := e.Resume(ctx, first, queued.ID)
		require.NoError(t, err)
		done <- run
	}()
	require.Eventually(t, func() bool {
		provider.mu.Lock()
		defer provider.mu.Unlock()
		return len(provider.jobs) == 1
	}, 5*time.Second, 10*time.Millisecond)
	stop()
	run := <-done
	require.Equal(t, model.RunRunning, run.State, "serve stopping cancels nothing")

	second := session(t, e)
	next, found, err := e.Next(t.Context(), second)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, queued.ID, next.ID, "a run whose driver is gone comes first")
	provider.mu.Lock()
	provider.wait = false
	provider.mu.Unlock()
	run, err = e.Resume(t.Context(), second, queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State)
	require.Len(t, provider.jobs, 2)
	require.Equal(t, 2, provider.jobs[1].Execution.Attempt, "the interrupted execution counts as an attempt")
}

func TestServeTakesPeoplesChecksFirst(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	first := queuedHarborRun(t, e, tahoeArm)
	branch, err := e.Branch(t.Context(), first.Branch)
	require.NoError(t, err)
	byServe, err := e.Enqueue(t.Context(), branch, mustPlan(t, e, first), model.OriginServe)
	require.NoError(t, err)
	byPerson, err := e.Enqueue(t.Context(), branch, mustPlan(t, e, first), model.OriginPerson)
	require.NoError(t, err)

	s := session(t, e)
	var order []int
	for {
		next, found, err := e.Next(t.Context(), s)
		require.NoError(t, err)
		if !found {
			break
		}
		order = append(order, next.Number)
		_, err = e.Resume(t.Context(), s, next.ID)
		require.NoError(t, err)
	}
	require.Equal(t, []int{first.Number, byPerson.Number, byServe.Number}, order)
}

// together is a scripted provider that builds two environments at once,
// as Tart builds two releases in the Mac's two VMs: each build waits until
// the other has begun, which it never would one at a time.
type together struct {
	scriptedProvider
	begun sync.WaitGroup
}

func (p *together) Parallel() int { return 2 }

func (p *together) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) error {
	p.begun.Done()
	waited := make(chan struct{})
	go func() { p.begun.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		return errors.New("the other environment never began: they built one at a time")
	case <-ctx.Done():
		return ctx.Err()
	}
	return p.scriptedProvider.Execute(ctx, job, build)
}

// A check's environments build together where their provider can, each
// with its own execution and results.
func TestEnvironmentsBuildTogetherWhereTheProviderCan(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	provider := &together{}
	provider.begun.Add(2)
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm, tahoeX86)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	require.Len(t, provider.jobs, 2)
	require.Equal(t, model.OutcomePassed, outcomes(t, e, run)["libharbor@arm64"])
	require.Equal(t, model.OutcomePassed, outcomes(t, e, run)["libharbor@x86_64"])
}

// A check whose every target in an environment would read what an earlier
// build of it read reuses that build's results, starting nothing
// (decision 28); a remade environment, a changed dependency, or --fresh
// builds again.
func TestAnUnchangedBuildIsReused(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	tools := model.ActivePort{Name: "harbor-tools", Spec: "@1_0", Directory: "graphics/harbor-tools", Archive: "sha256:aa"}
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{tools}}, identity: "origin a"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	first, err := e.Drive(t.Context(), session(t, e), queuedHarborRun(t, e, tahoeArm).ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, first.State, first.Detail)
	require.Len(t, provider.jobs, 1)

	var branch model.Branch
	var revision model.Revision
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		if revision, err = r.Revision(first.Revision); err != nil {
			return err
		}
		branch, err = r.Branch(first.Branch)
		return err
	}))
	again := func(fresh bool) model.Run {
		t.Helper()
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}, Fresh: fresh})
		require.NoError(t, err)
		queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
		require.NoError(t, err)
		run, err := e.Drive(t.Context(), session(t, e), queued.ID)
		require.NoError(t, err)
		require.Equal(t, model.RunPassed, run.State, run.Detail)
		return run
	}
	executions := func(run model.Run) ([]model.GuestExecution, []model.TargetResult) {
		t.Helper()
		var executions []model.GuestExecution
		var results []model.TargetResult
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			if executions, err = r.Executions(run.ID); err != nil {
				return err
			}
			results, err = r.Results(executions[0].ID)
			return err
		}))
		return executions, results
	}

	second := again(false)
	require.Len(t, provider.jobs, 1, "nothing was built")
	reused, results := executions(second)
	require.Len(t, reused, 1)
	require.True(t, reused[0].Reused)
	require.Equal(t, "origin a", reused[0].Identity)
	built, original := executions(first)
	require.Len(t, results, len(original))
	for _, result := range results {
		require.Equal(t, built[0].ID, result.ReusedFrom, "%s names the build it reuses", result.Target)
		require.Equal(t, model.OutcomePassed, result.Outcome)
	}
	// What a reviewer reads names the build, and the check that reused it.
	evidence, found, err := e.EvidenceFor(t.Context(), branch.ID, revision.Source.Tree)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, second.ID, evidence.Run.ID)
	require.Empty(t, evidence.Failed())
	shown, reusedIn := evidence.Built(0, evidence.Runs(0))
	require.Equal(t, []model.ExecutionID{built[0].ID}, []model.ExecutionID{shown[0].ID})
	require.Contains(t, runWords(shown, evidence.Checks(), reusedIn), string(built[0].ID)+" - checked in check-1, reused in check-2")

	again(true)
	require.Len(t, provider.jobs, 2, "--fresh builds")

	provider.identity = "origin b"
	again(false)
	require.Len(t, provider.jobs, 3, "the environment was made again")

	// What every port may source, _resources, is read by every build:
	// changed, each builds again, and is reused in its turn.
	write(t, branch.Worktree, map[string]string{"_resources/port1.0/group/harbor-1.0.tcl": "# changed\n"})
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	revision = capture.Revision
	again(false)
	require.Len(t, provider.jobs, 4, "_resources changed")
	again(false)
	require.Len(t, provider.jobs, 4, "and the new build is reused in its turn")
}
