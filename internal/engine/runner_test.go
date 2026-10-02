package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/prdescription"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
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
	// active are reported as the ports active as each target built, and
	// consumes as one target's, in its place.
	active   []model.ActivePort
	consumes map[model.TargetID][]model.ActivePort
	// keep makes each passed target's archive "<target>'s archive", kept.
	keep bool
	// says is reported as the build starts, as staging a guest's index
	// is, with a line behind the scenes after it.
	says string
	// logs and details are what a target's result names as its log, and
	// what the provider said of it.
	logs, details map[model.TargetID]string
	// fetches are the commits Git-fetched targets' builds say they
	// checked out.
	fetches map[model.TargetID]string
	// refused makes each failure one another attempt won't fix, as a
	// guest refusing dockhand's login is.
	refused bool
	jobs    []buildenv.Job
}

// scriptedArchive is the archive a scripted build of a target makes: its
// name, content, and digest.
func scriptedArchive(target model.TargetID) (string, []byte, string) {
	content := []byte(string(target) + "'s archive")
	sum := sha256.Sum256(content)
	return string(target) + "-1_0.darwin_25.arm64.tbz2", content, "sha256:" + hex.EncodeToString(sum[:])
}

func (p *scriptedProvider) Name() string { return "command" }

func (p *scriptedProvider) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) error {
	p.mu.Lock()
	p.jobs = append(p.jobs, job)
	failing := p.failures > 0
	p.failures--
	p.mu.Unlock()
	if failing && p.refused {
		return fmt.Errorf("%w: %w: reaching the VM: the guest refused SSH", buildenv.ErrInfrastructure, buildenv.ErrNeedsAttention)
	}
	if failing && !p.partial {
		return errors.New("the VM did not start")
	}
	if p.says != "" {
		progress.Report(ctx, "%s", p.says)
		progress.VerboseReport(ctx, "behind the scenes of %s", p.says)
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
		result := model.TargetResult{Target: target.ID, Outcome: outcome, Tests: model.TestsNone, Log: p.logs[target.ID], Detail: p.details[target.ID]}
		if outcome == model.OutcomeFailed {
			result.Phase = model.PhaseInstall
		}
		if p.active != nil {
			active := p.active
			if found, ok := p.consumes[target.ID]; ok {
				active = found
			}
			build.Consumed(target.ID, active)
			result.Archive = "sha256:" + string(target.ID)
		}
		name, content, digest := scriptedArchive(target.ID)
		if p.keep {
			result.Archive = digest
		}
		if commit, ok := p.fetches[target.ID]; ok {
			build.Fetched(target.ID, commit)
		}
		if err := build.Record(result); err != nil {
			return err
		}
		if p.keep && outcome == model.OutcomePassed {
			if err := build.Keep(target.ID, name, func(path string) error { return os.WriteFile(path, content, 0o644) }); err != nil {
				return err
			}
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

// Trouble another attempt won't fix, as a guest refusing dockhand's
// login, is said after one attempt, where every attempt was spent on it
// (the code-organization review's finding 7).
func TestTroubleAnotherAttemptWontFixIsTriedOnce(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	provider := &scriptedProvider{failures: 99, refused: true}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)

	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunAttention, run.State)
	require.Len(t, provider.jobs, 1, "one attempt")
	require.Contains(t, run.Detail, "the guest refused SSH; another attempt won't fix it, so none was made")
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
	candidates, err := e.Candidates(t.Context(), second)
	require.NoError(t, err)
	require.NotEmpty(t, candidates)
	require.Equal(t, queued.ID, candidates[0].ID, "a run whose driver is gone comes first")
	provider.mu.Lock()
	provider.wait = false
	provider.mu.Unlock()
	run, err = e.Resume(t.Context(), second, queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State)
	require.Len(t, provider.jobs, 2)
	require.Equal(t, 2, provider.jobs[1].Execution.Attempt, "the interrupted execution counts as an attempt")
}

// Serve's state says whether a serve leads, as which process, and whether
// it opens pull requests, as that serve said of itself rather than an
// earlier one; and the checks queued or running, with those whose process
// ended counted as stopped (the hugo exercise's certigo run, finding 6).
func TestServeStateSaysWhoLeadsAndWhatStopped(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	ctx := t.Context()
	observer := session(t, e)
	state, err := e.ServeState(ctx, observer)
	require.NoError(t, err)
	require.Equal(t, ServeState{}, state)

	run := queuedHarborRun(t, e, tahoeArm)
	driver := session(t, e)
	lease, err := driver.Acquire(ctx, RunResource(run.ID))
	require.NoError(t, err)
	require.NoError(t, driver.Fenced(ctx, lease, func(tx store.Tx) error {
		run.State = model.RunRunning
		return tx.UpdateRun(run)
	}))
	state, err = e.ServeState(ctx, observer)
	require.NoError(t, err)
	require.Equal(t, ServeState{Queue: 1}, state, "its process still runs it")
	require.NoError(t, driver.End(context.WithoutCancel(ctx)))
	state, err = e.ServeState(ctx, observer)
	require.NoError(t, err)
	require.Equal(t, ServeState{Queue: 1, Stopped: 1}, state, "its process ended without settling it")

	require.NoError(t, e.writeServeFile("serving.json", []byte(`{"pid": 1, "submit_passing": true}`)))
	_, err = session(t, e).Lead(ctx)
	require.NoError(t, err)
	state, err = e.ServeState(ctx, observer)
	require.NoError(t, err)
	require.Equal(t, ServeState{Running: true, PID: os.Getpid(), Queue: 1, Stopped: 1}, state, "an earlier serve's word isn't the leader's")
	e.announceServing(true)
	state, err = e.ServeState(ctx, observer)
	require.NoError(t, err)
	require.Equal(t, ServeState{Running: true, PID: os.Getpid(), OpensPullRequests: true, Queue: 1, Stopped: 1}, state)
	e.announceServing(false)
	state, err = e.ServeState(ctx, observer)
	require.NoError(t, err)
	require.False(t, state.OpensPullRequests)
}

// What the work tells a person as a run builds, as staging the index a
// guest takes, is the run's progress, for whoever follows it with wait. A
// command driving its own check still shows it only when asked (the hugo
// exercise's certigo run, finding 7).
func TestWhatTheWorkReportsIsTheRunsProgress(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	says := "Building the PortIndex; this may take several minutes"
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{says: says}}
	queued := queuedHarborRun(t, e, tahoeArm)
	var shown []progress.Update
	driving := progress.Quiet(progress.WithReporter(t.Context(), func(update progress.Update) { shown = append(shown, update) }))
	run, err := e.Drive(driving, session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)

	events, err := e.RunEvents(t.Context(), run.ID, 0)
	require.NoError(t, err)
	var kept []string
	for _, event := range events {
		if event.Kind == "progress" {
			kept = append(kept, event.Message)
		}
	}
	require.Equal(t, []string{describeEnvironment(tahoeArm) + ": " + says}, kept, "what's behind the scenes isn't kept")
	// The command shows what the run keeps through the run, and -v the
	// rest, each saying which environment it's about, since two stage at
	// once (the hugo exercise's check-64).
	about := describeEnvironment(tahoeArm)
	require.Contains(t, shown, progress.Update{Level: progress.Debug, Message: about + ": " + says, About: about})
	require.Contains(t, shown, progress.Update{Level: progress.Verbose, Message: about + ": behind the scenes of " + says, About: about})
}

// A failure's detail gains what its log most likely says made it fail,
// marked as read from the log, beside what the provider said: whichever
// provider built it, the log is on this Mac (D10, from the beekeeper-studio
// run's finding 2).
func TestAFailuresDetailSaysWhatItsLogShows(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	log := filepath.Join(t.TempDir(), "harbor-cli.log")
	require.NoError(t, os.WriteFile(log, []byte("--->  Building harbor-cli\nsrc/cli.c:3:1: error: expected ';' after expression\nmake: *** [all] Error 1\n"), 0o644))
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{
		outcomes: map[model.TargetID]model.Outcome{"harbor-cli": model.OutcomeFailed},
		logs:     map[model.TargetID]string{"harbor-cli": log, "libharbor": log},
		details:  map[model.TargetID]string{"harbor-cli": "`make` failed with exit code: 2"},
	}}
	queued := queuedHarborRun(t, e, tahoeArm)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	details := map[model.TargetID]string{}
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		plan, err := r.Plan(run.Plan)
		require.NoError(t, err)
		evidence, err := runEvidence(r, run, plan)
		require.NoError(t, err)
		for _, target := range evidence.Targets {
			details[target.Target.ID] = target.Outcomes[0].Detail
		}
		return nil
	}))
	require.Equal(t, "`make` failed with exit code: 2 · from its log: src/cli.c:3:1: error: expected ';' after expression", details["harbor-cli"])
	require.Empty(t, details["libharbor"], "a target that passed has no cause, whatever its log")
	require.Equal(t, "from its log: src/cli.c:3:1: error: expected ';' after expression", withCause("", model.TargetResult{Log: log}), "with nothing else said")
	require.Equal(t, "said", withCause("said", model.TargetResult{Log: filepath.Join(t.TempDir(), "gone.log")}), "a log that can't be read says nothing")

	// Read from where the step that failed began: an error a dependency's
	// build printed before it, and went on from, isn't its cause.
	stepped := filepath.Join(t.TempDir(), "hugo.log")
	require.NoError(t, os.WriteFile(stepped, []byte("dep/probe.c:1:1: error: tried and went on\n--->  Building hugo\ncmd/main.c:9:2: error: hugo's own\n"), 0o644))
	steps := []model.LogStep{{Name: model.StepDependencies, Line: 1}, {Name: model.StepInstall, Line: 2}}
	require.Equal(t, "from its log: cmd/main.c:9:2: error: hugo's own", withCause("", model.TargetResult{Log: stepped, Steps: steps}))
	require.Equal(t, "from its log: dep/probe.c:1:1: error: tried and went on", withCause("", model.TargetResult{Log: stepped}), "without steps, from the top")
}

func TestServeTakesPeoplesChecksFirst(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	first := queuedHarborRun(t, e, tahoeArm)
	branch, err := e.Branch(t.Context(), first.Branch)
	require.NoError(t, err)
	// A branch has one check at a time, so the queue's others are other
	// branches': each recorded here with the same work as the first.
	other := func(name string, origin model.Origin) model.Run {
		t.Helper()
		var run model.Run
		require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
			revision, err := tx.Revision(first.Revision)
			if err != nil {
				return err
			}
			started := branch
			started.ID, started.Name, started.Worktree = model.BranchID(store.NewID("br")), model.BranchPrefix+name, filepath.Join(t.TempDir(), name)
			if err := tx.AddBranch(started); err != nil {
				return err
			}
			revision.ID, revision.Branch = model.RevisionID(store.NewID("rev")), started.ID
			if err := tx.AddRevision(revision); err != nil {
				return err
			}
			plan := mustPlan(t, e, first)
			plan.Revision = revision.ID
			if err := tx.AddPlan(plan); err != nil {
				return err
			}
			run, err = e.queue(tx, model.Run{Branch: started.ID, Revision: revision.ID, Plan: plan.ID, Origin: origin}, "queued")
			return err
		}))
		return run
	}
	byServe := other("harbor-serve", model.OriginServe)
	byPerson := other("harbor-person", model.OriginPerson)

	s := session(t, e)
	var order []int
	for {
		candidates, err := e.Candidates(t.Context(), s)
		require.NoError(t, err)
		if len(candidates) == 0 {
			break
		}
		order = append(order, candidates[0].Number)
		_, err = e.Resume(t.Context(), s, candidates[0].ID)
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
	require.Equal(t, []prdescription.Run{{ID: string(built[0].ID), Ref: built[0].ProviderRef, Check: "check-1", ReusedIn: "check-2"}},
		report(model.Environment{}, model.Observed{}, shown, evidence.Checks(), reusedIn).Runs)

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

// Where only some targets would read what their earlier builds read, those
// reuse their results and the rest build, in one execution: the provider is
// given only what builds. A target one that builds needs is built with it,
// since a reused build isn't in the guest: what the plan says it needs, and
// what was active as it last built, which it may reach through ports the
// branch doesn't change (decision 28).
func TestTheTargetsThatChangedBuildAndTheRestAreReused(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	lib := model.ActivePort{Name: "libharbor", Spec: "@4_0", Directory: "devel/libharbor", Archive: "sha256:aa"}
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{}, consumes: map[model.TargetID][]model.ActivePort{"harbor-cli": {lib}}}, identity: "origin a"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	revision := harborBranch(t, e)
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		branch, err = r.Branch(revision.Branch)
		return err
	}))
	// Later captures compare with the base the fixture's ports were added
	// on, as its own did.
	branch.Base = revision.Source.Base
	// harbor-cli needs libharbor, as the plan says; harbor-viewer needs
	// neither.
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{
		"devel/libharbor":        {port("libharbor")},
		"devel/harbor-cli":       {port("harbor-cli", "libharbor")},
		"graphics/harbor-viewer": {port("harbor-viewer")},
	}}
	check := func() (model.Run, []model.TargetID, map[model.TargetID]model.ExecutionID, []model.GuestExecution) {
		t.Helper()
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		require.Empty(t, plan.Unresolved)
		queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
		require.NoError(t, err)
		jobs := len(provider.jobs)
		run, err := e.Drive(t.Context(), session(t, e), queued.ID)
		require.NoError(t, err)
		require.Equal(t, model.RunPassed, run.State, run.Detail)
		var built []model.TargetID
		for _, job := range provider.jobs[jobs:] {
			for _, target := range job.Targets {
				built = append(built, target.ID)
			}
		}
		from := map[model.TargetID]model.ExecutionID{}
		var executions []model.GuestExecution
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			if executions, err = r.Executions(run.ID); err != nil {
				return err
			}
			for _, execution := range executions {
				results, err := r.Results(execution.ID)
				if err != nil {
					return err
				}
				for _, result := range results {
					from[result.Target] = result.ReusedFrom
				}
			}
			return nil
		}))
		return run, built, from, executions
	}
	change := func(files map[string]string) {
		t.Helper()
		write(t, branch.Worktree, files)
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
		require.NoError(t, err)
		revision = capture.Revision
	}

	_, built, _, first := check()
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli", "harbor-viewer"}, built)

	change(map[string]string{"graphics/harbor-viewer/files/a.patch": "c\n"})
	second, built, from, executions := check()
	require.Equal(t, []model.TargetID{"harbor-viewer"}, built, "only the port that changed builds")
	require.Equal(t, map[model.TargetID]model.ExecutionID{"libharbor": first[0].ID, "harbor-cli": first[0].ID, "harbor-viewer": ""}, from)
	require.Len(t, executions, 1, "one execution builds the rest and holds what it reused")
	require.False(t, executions[0].Reused, "it built something")
	evidence, found, err := e.EvidenceFor(t.Context(), branch.ID, revision.Source.Tree)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, second.ID, evidence.Run.ID)
	shown, reusedIn := evidence.Built(0, evidence.Runs(0))
	require.Equal(t, []model.ExecutionID{first[0].ID, executions[0].ID}, []model.ExecutionID{shown[0].ID, shown[1].ID}, "each result shows the run that built it")
	require.Equal(t, map[model.ExecutionID]string{first[0].ID: "check-2"}, reusedIn)

	change(map[string]string{"devel/harbor-cli/Portfile": "name harbor-cli\nrevision 2\n"})
	_, built, from, third := check()
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli"}, built, "harbor-cli builds, and takes libharbor, which the plan says it needs")
	require.Equal(t, executions[0].ID, from["harbor-viewer"], "harbor-viewer reuses its build in check-2, not check-2's reuse of another")

	// harbor-viewer comes to have libharbor active as it builds, through a
	// port the branch doesn't change: once it has built that way, it takes
	// libharbor with it.
	provider.consumes["harbor-viewer"] = []model.ActivePort{lib}
	change(map[string]string{"graphics/harbor-viewer/files/a.patch": "d\n"})
	_, built, _, _ = check()
	require.Equal(t, []model.TargetID{"harbor-viewer"}, built, "its last build had libharbor inactive")
	change(map[string]string{"graphics/harbor-viewer/files/a.patch": "e\n"})
	_, built, from, _ = check()
	require.Equal(t, []model.TargetID{"libharbor", "harbor-viewer"}, built, "its last build had libharbor active")
	require.Equal(t, third[0].ID, from["harbor-cli"], "harbor-cli reuses its build in check-3, not check-4's reuse of it")
}

// A reused target that one that builds needs is installed in the guest
// from the archive kept of its build, rather than built again; so is one
// an earlier attempt finished, on a retry (decisions 28 and 44). A build
// that shows it active from another archive is said to have been given
// another by MacPorts.
func TestTheGuestInstallsWhatABuildNeedsFromItsKeptArchive(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	_, _, libDigest := scriptedArchive("libharbor")
	lib := model.ActivePort{Name: "libharbor", Spec: "@4_0", Directory: "devel/libharbor", Archive: libDigest}
	provider := &identified{scriptedProvider: scriptedProvider{keep: true, active: []model.ActivePort{}, consumes: map[model.TargetID][]model.ActivePort{"harbor-cli": {lib}}}, identity: "origin a"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	revision := harborBranch(t, e)
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		branch, err = r.Branch(revision.Branch)
		return err
	}))
	branch.Base = revision.Source.Base
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{
		"devel/libharbor":        {port("libharbor")},
		"devel/harbor-cli":       {port("harbor-cli", "libharbor")},
		"graphics/harbor-viewer": {port("harbor-viewer")},
	}}
	check := func(fresh bool) model.Run {
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
	targets := func(job buildenv.Job) []model.TargetID {
		var ids []model.TargetID
		for _, target := range job.Targets {
			ids = append(ids, target.ID)
		}
		return ids
	}
	messages := func(run model.Run) []string {
		t.Helper()
		events, err := e.RunEvents(t.Context(), run.ID, 0)
		require.NoError(t, err)
		var all []string
		for _, event := range events {
			all = append(all, event.Message)
		}
		return all
	}

	check(false)
	require.Len(t, provider.jobs, 1)
	require.Empty(t, provider.jobs[0].Installs, "everything builds, nothing is installed")

	write(t, branch.Worktree, map[string]string{"devel/harbor-cli/Portfile": "name harbor-cli\nrevision 2\n"})
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	revision = capture.Revision
	second := check(false)
	require.Len(t, provider.jobs, 2)
	job := provider.jobs[1]
	require.Equal(t, []model.TargetID{"harbor-cli"}, targets(job), "libharbor is reused, not built")
	require.Len(t, job.Installs, 1)
	installed := job.Installs[0]
	require.Equal(t, buildenv.Archive{Target: "libharbor", Port: "libharbor", Name: "libharbor-1_0.darwin_25.arm64.tbz2", Digest: libDigest, Path: installed.Path}, installed)
	data, err := os.ReadFile(installed.Path)
	require.NoError(t, err)
	require.Equal(t, "libharbor's archive", string(data), "the archive kept of its build")
	require.NotContains(t, strings.Join(messages(second), "\n"), "another archive", "harbor-cli built with the archive it was given")

	// A build that shows libharbor active from another archive: MacPorts
	// gave it another.
	provider.consumes["harbor-cli"] = []model.ActivePort{{Name: "libharbor", Spec: "@4_0", Directory: "devel/libharbor", Archive: "sha256:upstream"}}
	write(t, branch.Worktree, map[string]string{"devel/harbor-cli/Portfile": "name harbor-cli\nrevision 3\n"})
	capture, err = e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	revision = capture.Revision
	third := check(false)
	require.Contains(t, strings.Join(messages(third), "\n"), "harbor-cli built with libharbor from another archive than the one kept of its build: MacPorts chose sha256:upstream")

	// A retry installs what the attempt before it finished.
	provider.consumes["harbor-cli"] = []model.ActivePort{lib}
	provider.failures, provider.partial = 1, true
	check(true)
	retry := provider.jobs[len(provider.jobs)-1]
	require.Equal(t, 2, retry.Execution.Attempt)
	require.Equal(t, []model.TargetID{"harbor-cli", "harbor-viewer"}, targets(retry))
	require.Len(t, retry.Installs, 1)
	require.Equal(t, model.TargetID("libharbor"), retry.Installs[0].Target, "libharbor, which the first attempt built and kept")
}

// An active port the guest couldn't place in the ports tree is recorded
// with no directory, so its build's inputs are incomplete: a later check
// doesn't reuse that build, and builds again rather than failing.
func TestABuildReadingAPortOutsideTheTreeIsBuiltAgain(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	elsewhere := model.ActivePort{Name: "harbor-legacy", Spec: "@1_0", Archive: "sha256:aa"}
	provider := &identified{scriptedProvider: scriptedProvider{active: []model.ActivePort{elsewhere}}, identity: "origin a"}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm)
	first, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, first.State, first.Detail)

	var branch model.Branch
	var revision model.Revision
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		if revision, err = r.Revision(first.Revision); err != nil {
			return err
		}
		branch, err = r.Branch(first.Branch)
		return err
	}))
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}})
	require.NoError(t, err)
	again, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	second, err := e.Drive(t.Context(), session(t, e), again.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, second.State, second.Detail)
	require.Len(t, provider.jobs, 2, "built again, not reused")
}

// indexing builds two environments together, each asking first for what
// the engine assembles on first use, as Tart's ask for the port index.
type indexing struct {
	together
	index func() error
}

func (p *indexing) Execute(ctx context.Context, job buildenv.Job, build buildenv.Build) error {
	if err := p.index(); err != nil {
		return err
	}
	return p.together.Execute(ctx, job, build)
}

// Environments building together share what the engine assembles on
// first use: go test -race sees them race for it without the engine's
// lock.
func TestEnvironmentsBuildingTogetherShareWhatTheEngineAssembles(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	t.Setenv("DOCKHAND_INDEX_CACHE", t.TempDir())
	provider := &indexing{index: func() error { _, err := e.PortIndex(); return err }}
	provider.begun.Add(2)
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued := queuedHarborRun(t, e, tahoeArm, tahoeX86)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
}

// A branch has one check at a time (decision 29): another is refused while
// one is queued or running, for the same files or others, by Enqueue or
// Retry alike. A baseline looks beside the check it explains, and a check
// asked to stop no longer counts.
func TestABranchHasOneCheckAtATime(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	first := queuedHarborRun(t, e, tahoeArm)
	branch, err := e.Branch(t.Context(), first.Branch)
	require.NoError(t, err)

	var active *ActiveRunError
	_, err = e.Enqueue(t.Context(), branch, mustPlan(t, e, first), model.OriginPerson)
	require.ErrorAs(t, err, &active)
	require.True(t, active.SameRevision)
	require.EqualError(t, err, "check-1 is already queued for these files; dockhand wait check-1 follows it")

	write(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 5\n"})
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	edited, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
	require.NoError(t, err)
	_, err = e.Enqueue(t.Context(), branch, edited, model.OriginServe)
	require.ErrorAs(t, err, &active)
	require.False(t, active.SameRevision)
	require.Regexp(t, `^check-1 is queued for snapshot \d+, and a branch has one check at a time; dockhand cancel check-1 stops it, keeping what it finished$`, err.Error())

	baseline, err := e.enqueue(t.Context(), branch, mustPlan(t, e, first), model.OriginPerson, first.ID)
	require.NoError(t, err, "a baseline looks beside the check it explains")
	require.Equal(t, first.ID, baseline.BaselineOf)

	done, err := e.Drive(t.Context(), session(t, e), first.ID)
	require.NoError(t, err)
	require.True(t, done.State.Terminal())
	second, err := e.Enqueue(t.Context(), branch, edited, model.OriginPerson)
	require.NoError(t, err, "once the first is done, the next may queue")
	_, err = e.Retry(t.Context(), done)
	require.ErrorAs(t, err, &active, "a retry is the branch's check too")
	require.Equal(t, second.ID, active.Run.ID)

	_, err = e.RequestCancel(t.Context(), session(t, e), second.ID)
	require.NoError(t, err)
	_, err = e.Retry(t.Context(), done)
	require.NoError(t, err, "a check asked to stop no longer counts")
}

// An environment whose provider can't say what it is builds every target,
// and says why where another environment of the check could reuse: gh's
// re-check reused its Tart results and ran GitHub's workflow again, 5.5
// of its 5.7 minutes, without a word (the gh rebase's finding 1). Where no
// environment of the check could, it's nothing to remark on.
func TestAnEnvironmentThatCantReuseSaysSo(t *testing.T) {
	github := model.Environment{Provider: "github", Platform: tahoeArm.Platform}
	said := func(t *testing.T, e *Engine) []string {
		t.Helper()
		events, err := e.Events(t.Context(), 0)
		require.NoError(t, err)
		var messages []string
		for _, event := range events {
			if strings.Contains(event.Message, "builds every target") {
				messages = append(messages, event.Message)
			}
		}
		return messages
	}

	f := setup(t)
	e := f.open(t)
	e.Providers = map[string]buildenv.Provider{"command": &identified{identity: "origin a"}, "github": &scriptedProvider{}}
	run, err := e.Drive(t.Context(), session(t, e), queuedHarborRun(t, e, tahoeArm, github).ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State, run.Detail)
	messages := said(t, e)
	require.Len(t, messages, 1)
	require.Equal(t, run.Name()+": "+describeEnvironment(github)+": builds every target, since its provider doesn't say what the environment is, so no earlier result can be reused there", messages[0])

	f = setup(t)
	e = f.open(t)
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}, "github": &scriptedProvider{}}
	_, err = e.Drive(t.Context(), session(t, e), queuedHarborRun(t, e, tahoeArm, github).ID)
	require.NoError(t, err)
	require.Empty(t, said(t, e), "no environment could reuse")

	f = setup(t)
	e = f.open(t)
	e.Providers = map[string]buildenv.Provider{"command": &identified{identity: "origin a"}}
	_, err = e.Drive(t.Context(), session(t, e), queuedHarborRun(t, e, tahoeArm, tahoeX86).ID)
	require.NoError(t, err)
	require.Empty(t, said(t, e), "every environment could")
}

// Serve's banner counts checks, and says where one builds several of its
// environments at once, as Tart builds two releases (the sshuttle run).
func TestServeSaysHowManyChecksAndEnvironmentsAtOnce(t *testing.T) {
	require.Equal(t, "tart (1 check at a time, each building up to 2 of its environments at once)", capacityWords("tart", &together{}, 1))
	require.Equal(t, "command (2 checks at a time)", capacityWords("command", &scriptedProvider{}, 2))
}
