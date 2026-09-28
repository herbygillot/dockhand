package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/reuse"
	"github.com/herbygillot/dockhand/internal/store"
)

// RunResource is the lease a run's driver holds.
func RunResource(id model.RunID) string { return "run:" + string(id) }

// Enqueue records a check request: its plan and a queued run. The request
// is immutable from here; editing the branch never changes what it means.
// A branch has one check at a time (decision 29), so it refuses while
// another is queued or running (ActiveRunError).
func (e *Engine) Enqueue(ctx context.Context, branch model.Branch, plan model.Plan, origin model.Origin) (model.Run, error) {
	return e.enqueue(ctx, branch, plan, origin, "")
}

func (e *Engine) enqueue(ctx context.Context, branch model.Branch, plan model.Plan, origin model.Origin, baselineOf model.RunID) (model.Run, error) {
	var run model.Run
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.AddPlan(plan); err != nil {
			return err
		}
		var err error
		run, err = e.queue(tx, model.Run{Branch: branch.ID, Revision: plan.Revision, Plan: plan.ID, Origin: origin, BaselineOf: baselineOf}, "queued for "+branch.ShortName())
		return err
	})
	return run, err
}

// queue records run as queued, numbered and stamped now, saying why in the
// journal. A branch has one check at a time (decision 29): a run that isn't
// a baseline, which looks beside the check it explains, is refused while
// another of the branch's is queued or running and not asked to stop.
func (e *Engine) queue(tx store.Tx, run model.Run, why string) (model.Run, error) {
	if run.BaselineOf == "" {
		active, err := tx.Runs(store.RunFilter{Branch: run.Branch, States: []model.RunState{model.RunQueued, model.RunRunning}})
		if err != nil {
			return run, err
		}
		for _, other := range active {
			if other.BaselineOf != "" || other.CancelRequested != nil {
				continue
			}
			checking, err := tx.Revision(other.Revision)
			if err != nil {
				return run, err
			}
			return run, &ActiveRunError{Run: other, Checking: checking, SameRevision: other.Revision == run.Revision}
		}
	}
	number, err := tx.NextRunNumber()
	if err != nil {
		return run, err
	}
	run.ID, run.Number, run.State, run.CreatedAt = model.RunID(store.NewID("run")), number, model.RunQueued, e.now()
	if err := tx.AddRun(run); err != nil {
		return run, err
	}
	_, err = tx.AppendEvent(model.Event{At: run.CreatedAt, Branch: run.Branch, Run: run.ID, Kind: "run.state", Level: model.LevelInfo,
		Message: run.Name() + " " + why})
	return run, err
}

// ActiveRunError is the branch's check already queued or running, which a
// new one for the branch would stand beside: a branch has one check at a
// time (decision 29).
type ActiveRunError struct {
	Run model.Run
	// Checking is what it checks, and SameRevision whether that is what
	// the new check would have.
	Checking     model.Revision
	SameRevision bool
}

func (a *ActiveRunError) Error() string {
	name := a.Run.Name()
	if a.SameRevision {
		return fmt.Sprintf("%s is already %s for these files; dockhand wait %s follows it", name, a.Run.State, name)
	}
	return fmt.Sprintf("%s is %s for %s, and a branch has one check at a time; dockhand cancel %s stops it, keeping what it finished", name, a.Run.State, Describe(a.Checking), name)
}

// RequestCancel records that a run should stop. A queued run is canceled
// at once; a running one is stopped by whoever holds it, or by this
// session when no one does.
func (e *Engine) RequestCancel(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	var run model.Run
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		var err error
		if run, err = tx.Run(id); err != nil {
			return err
		}
		if run.State.Terminal() || run.CancelRequested != nil {
			return nil
		}
		now := e.now()
		run.CancelRequested = &now
		if run.State == model.RunQueued {
			run.State, run.FinishedAt, run.Detail = model.RunCanceled, &now, "canceled before it started"
		}
		if err := tx.UpdateRun(run); err != nil {
			return err
		}
		_, err = tx.AppendEvent(model.Event{At: now, Session: session.ID(), Branch: run.Branch, Run: run.ID, Kind: "run.cancel", Level: model.LevelInfo, Message: run.Name() + ": cancel requested"})
		return err
	})
	if err != nil || run.State.Terminal() {
		return run, err
	}
	// With no one attending the run, this session applies the cancel.
	lease, holder, err := session.TakeIfUnattended(ctx, RunResource(id))
	if err != nil || holder != nil {
		return run, err
	}
	defer session.Release(context.WithoutCancel(ctx), lease)
	return e.finishCanceled(ctx, session, lease, id)
}

func (e *Engine) finishCanceled(ctx context.Context, session *coord.Session, lease model.Lease, id model.RunID) (model.Run, error) {
	var run model.Run
	err := session.Fenced(ctx, lease, func(tx store.Tx) error {
		var err error
		if run, err = tx.Run(id); err != nil || run.State.Terminal() {
			return err
		}
		executions, err := tx.Executions(id)
		if err != nil {
			return err
		}
		now := e.now()
		for _, execution := range executions {
			if !execution.State.Terminal() {
				execution.State, execution.FinishedAt, execution.Detail = model.ExecutionCanceled, &now, "canceled"
				if err := tx.UpdateExecution(execution); err != nil {
					return err
				}
			}
		}
		if run.State == model.RunQueued {
			run.State = model.RunRunning
			if err := tx.UpdateRun(run); err != nil {
				return err
			}
		}
		run.State, run.FinishedAt, run.Detail = model.RunCanceled, &now, "canceled; finished results are kept"
		if err := tx.UpdateRun(run); err != nil {
			return err
		}
		_, err = session.Emit(tx, model.Event{Branch: run.Branch, Run: run.ID, Kind: "run.state", Level: model.LevelInfo, Message: run.Name() + " canceled"})
		return err
	})
	return run, err
}

// Drive runs a run to its end under this session's lease on it (Design v3
// §7, §11): one guest execution per environment, each target's result
// checkpointed as it finishes, infrastructure failures tried again up to
// model.MaxAttempts without repeating any verdict, and a cancel request
// applied at the next step. Canceling ctx stops the run the same way and
// keeps what finished.
func (e *Engine) Drive(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	return e.drive(ctx, session, id, false)
}

// Resume drives a run as Drive does, except that when ctx ends without a
// cancel request, the run is left running for the next driver: serve
// stopping, or its Mac restarting, loses nothing (Design v3 §11). The
// execution it interrupted counts as one attempt.
func (e *Engine) Resume(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	return e.drive(ctx, session, id, true)
}

func (e *Engine) drive(ctx context.Context, session *coord.Session, id model.RunID, resumable bool) (model.Run, error) {
	lease, err := session.Acquire(ctx, RunResource(id))
	if err != nil {
		return model.Run{}, err
	}
	defer session.Release(context.WithoutCancel(ctx), lease)
	d := &driver{e: e, session: session, lease: lease, resumable: resumable}
	return d.drive(ctx, id)
}

type driver struct {
	e       *Engine
	session *coord.Session
	lease   model.Lease
	run     model.Run
	plan    model.Plan
	// resumable leaves a run whose driver stopped running, for the next.
	resumable bool
	// canceled records that a cancel request, not the driver stopping,
	// ended the run.
	canceled atomic.Bool
	// problems are why the run needs a person's attention; mu guards them
	// while environments build together.
	problems []string
	mu       sync.Mutex
}

// problem notes why the run needs a person's attention.
func (d *driver) problem(problem string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.problems = append(d.problems, problem)
}

func (d *driver) fenced(ctx context.Context, fn func(store.Tx) error) error {
	return d.session.Fenced(context.WithoutCancel(ctx), d.lease, fn)
}

func (d *driver) emit(ctx context.Context, kind, message string) {
	_ = d.fenced(ctx, func(tx store.Tx) error {
		_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: kind, Level: model.LevelInfo, Message: message})
		return err
	})
}

func (d *driver) drive(ctx context.Context, id model.RunID) (model.Run, error) {
	e := d.e
	var revision model.Revision
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if d.run, err = r.Run(id); err != nil {
			return err
		}
		if d.plan, err = r.Plan(d.run.Plan); err != nil {
			return err
		}
		revision, err = r.Revision(d.run.Revision)
		return err
	}); err != nil {
		return model.Run{}, err
	}
	if d.run.State.Terminal() {
		return d.run, nil
	}
	if d.run.CancelRequested != nil {
		return e.finishCanceled(ctx, d.session, d.lease, id)
	}
	if d.run.State == model.RunQueued {
		if err := d.fenced(ctx, func(tx store.Tx) error {
			d.run.State = model.RunRunning
			if err := tx.UpdateRun(d.run); err != nil {
				return err
			}
			_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: "run.state", Level: model.LevelInfo, Message: d.run.Name() + " running"})
			return err
		}); err != nil {
			return d.run, err
		}
	}

	// A cancel recorded by another command stops this one at its next step.
	running, stop := context.WithCancel(ctx)
	defer stop()
	go d.watchCancel(running, stop)

	commit, err := e.revisionCommit(ctx, revision)
	if err != nil {
		return d.run, err
	}
	// Environments build together where their providers can: each
	// provider's at most as many at once as it says (ParallelProvider), in
	// the plan's order, one at a time otherwise; different providers' side
	// by side.
	var names []string
	queues := map[string][]model.Environment{}
	for _, environment := range d.plan.Environments {
		if _, ok := e.Providers[environment.Provider]; !ok {
			d.problem(fmt.Sprintf("no provider %q is set up here", environment.Provider))
			continue
		}
		if _, ok := queues[environment.Provider]; !ok {
			names = append(names, environment.Provider)
		}
		queues[environment.Provider] = append(queues[environment.Provider], environment)
	}
	group, together := errgroup.WithContext(running)
	for _, name := range names {
		provider, queue := e.Providers[name], queues[name]
		workers := 1
		if parallel, ok := provider.(buildenv.ParallelProvider); ok && parallel.Parallel() > 1 {
			workers = parallel.Parallel()
		}
		var next atomic.Int64
		for range min(workers, len(queue)) {
			group.Go(func() error {
				for {
					i := int(next.Add(1)) - 1
					if i >= len(queue) || together.Err() != nil {
						return nil
					}
					if err := d.environment(together, provider, queue[i], revision, commit); err != nil {
						return err
					}
				}
			})
		}
	}
	if err := group.Wait(); err != nil {
		return d.run, err
	}
	if running.Err() != nil {
		if d.stopped() {
			return e.Run(context.WithoutCancel(ctx), id)
		}
		// An interrupt cancels ctx too; the run is still settled.
		return e.finishCanceled(context.WithoutCancel(ctx), d.session, d.lease, id)
	}
	return d.finish(context.WithoutCancel(ctx))
}

// stopped reports that the driver itself is stopping, with no cancel
// request, in a driver that leaves such runs for the next one.
func (d *driver) stopped() bool { return d.resumable && !d.canceled.Load() }

// watchCancel polls the run for a cancel request.
func (d *driver) watchCancel(ctx context.Context, stop context.CancelFunc) {
	ticker := time.NewTicker(d.e.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = d.e.Store.View(ctx, d.e.Repository, func(r store.Reader) error {
				run, err := r.Run(d.run.ID)
				if err == nil && run.CancelRequested != nil {
					d.canceled.Store(true)
					stop()
				}
				return nil
			})
		}
	}
}

func (e *Engine) pollInterval() time.Duration {
	if e.options.Poll > 0 {
		return e.options.Poll
	}
	return time.Second
}

// environment runs the guest executions one environment needs.
func (d *driver) environment(ctx context.Context, provider buildenv.Provider, environment model.Environment, revision model.Revision, commit string) error {
	e := d.e
	for {
		var executions []model.GuestExecution
		var results map[model.TargetID]model.TargetResult
		if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
			var err error
			if executions, err = r.Executions(d.run.ID); err != nil {
				return err
			}
			executions = slices.DeleteFunc(executions, func(x model.GuestExecution) bool { return x.Environment != environment })
			results, err = mergedResults(r, executions)
			return err
		}); err != nil {
			return err
		}
		attempt := 0
		if len(executions) > 0 {
			last := executions[len(executions)-1]
			attempt = last.Attempt
			switch last.State {
			case model.ExecutionFinished, model.ExecutionCanceled:
				return nil
			case model.ExecutionWaiting, model.ExecutionRunning:
				// The process that ran it is gone, or this lease would not
				// have been granted.
				if err := d.endExecution(ctx, last, model.ExecutionInfrastructure, "the process running it ended"); err != nil {
					return err
				}
			}
		}
		// What is left to build here, in this environment's own order,
		// each target with what it needs built first here.
		planned, _ := d.plan.In(environment)
		var remaining []buildenv.Target
		for _, id := range planned.Order {
			if _, unmet := d.plan.UnmetIn(environment, id); unmet {
				continue
			}
			if result, ok := results[id]; !ok || !result.Outcome.Complete() {
				target, _ := d.plan.Target(id)
				remaining = append(remaining, buildenv.Target{PlanTarget: target, DependsOn: planned.Dependencies[id]})
			}
		}
		if len(remaining) == 0 {
			return nil
		}
		if attempt >= model.MaxAttempts {
			d.problem(fmt.Sprintf("%s failed %d times for reasons of its own; see dockhand logs %s", describeEnvironment(environment), attempt, d.run.Name()))
			return nil
		}
		// The environment's identity is read once for the attempt. Reuse
		// compares earlier builds' with it, and the execution records it as
		// it is when the execution begins: evidence compares it with the
		// environment's identity whenever it is judged (Counts).
		identity := e.identitiesNow(ctx, []model.Environment{environment})[environment]
		// Before the first attempt, the targets that would build as an
		// earlier build did reuse its result (decision 28). When every one
		// does, nothing is built.
		targets, paths, err := d.earlier(ctx, environment, remaining)
		if err != nil {
			return err
		}
		var reused map[model.TargetID]reuse.Candidate
		if attempt == 0 {
			choice, err := d.reusable(ctx, identity, targets, paths, revision.Source.Tree)
			if err != nil {
				return err
			}
			if len(choice.Reused) == len(remaining) {
				return d.recordReuse(ctx, environment, identity, remaining, choice.Reused)
			}
			reused = choice.Reused
		}
		// A provider run's ID is unique, and named for its provider:
		// tart_7y62p4sigena6xlr. The pull request names it, and dockhand
		// logs finds its evidence by it.
		execution := model.GuestExecution{ID: model.ExecutionID(store.NewID(environment.Provider)), Run: d.run.ID, Environment: environment, Identity: identity, Attempt: attempt + 1, State: model.ExecutionWaiting, CreatedAt: e.now()}
		if err := d.fenced(ctx, func(tx store.Tx) error {
			if err := tx.AddExecution(execution); err != nil {
				return err
			}
			execution.State = model.ExecutionRunning
			if err := tx.UpdateExecution(execution); err != nil {
				return err
			}
			_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: "execution.state", Level: model.LevelInfo,
				Message: fmt.Sprintf("%s: attempt %d of %d on %s, run %s", d.run.Name(), execution.Attempt, model.MaxAttempts, describeEnvironment(environment), execution.ID)})
			return err
		}); err != nil {
			return err
		}
		build := &build{d: d, ctx: ctx, execution: execution, tree: revision.Source.Tree, results: results, inputs: map[model.TargetID]model.TargetInputs{}}
		// The reused results are the execution's own, recorded before the
		// provider builds the rest.
		building := remaining
		if len(reused) > 0 {
			if err := build.reuses(remaining, reused); err != nil {
				return err
			}
			building = slices.DeleteFunc(slices.Clone(remaining), func(target buildenv.Target) bool {
				_, ok := reused[target.ID]
				return ok
			})
		}
		// What they need and don't build here, the guest installs from the
		// archives kept of it.
		build.installs, err = d.installs(ctx, environment, slices.DeleteFunc(targets, func(target reuse.Target) bool {
			return !slices.ContainsFunc(building, func(b buildenv.Target) bool { return b.ID == target.ID })
		}), build.results)
		if err != nil {
			return err
		}
		if len(build.installs) > 0 {
			var names []string
			for _, archive := range build.installs {
				names = append(names, string(archive.Target))
			}
			words := "the guest installs %s from the archives kept of their builds, for the targets that need them"
			if len(names) == 1 {
				words = "the guest installs %s from the archive kept of its build, for the targets that need it"
			}
			build.Progress(fmt.Sprintf(words, strings.Join(names, ", ")))
		}
		job := buildenv.Job{Run: d.run, Execution: execution, Revision: revision, Plan: d.plan, Environment: environment, Targets: building, Commit: commit, Installs: build.installs,
			Directory: filepath.Join(e.LogDirectory(), d.run.Name(), fmt.Sprintf("%s-%d", environmentSlug(environment), execution.Attempt))}
		err = provider.Execute(ctx, job, build)
		build.blockRemaining(building)
		// The build's copy holds what the provider reported.
		execution = build.execution
		switch {
		case ctx.Err() != nil && d.stopped():
			return d.endExecution(ctx, execution, model.ExecutionInfrastructure, "interrupted: the process driving it stopped")
		case ctx.Err() != nil:
			return d.endExecution(ctx, execution, model.ExecutionCanceled, "canceled")
		case err != nil:
			d.emit(ctx, "execution.retry", fmt.Sprintf("%s: %v", describeEnvironment(environment), err))
			if err := d.endExecution(ctx, execution, model.ExecutionInfrastructure, err.Error()); err != nil {
				return err
			}
			continue
		}
		return d.endExecution(ctx, execution, model.ExecutionFinished, "")
	}
}

func (d *driver) endExecution(ctx context.Context, execution model.GuestExecution, state model.ExecutionState, detail string) error {
	now := d.e.now()
	execution.State, execution.FinishedAt, execution.Detail = state, &now, detail
	return d.fenced(ctx, func(tx store.Tx) error { return tx.UpdateExecution(execution) })
}

// finish settles the run's state from its results.
func (d *driver) finish(ctx context.Context) (model.Run, error) {
	var evidence Evidence
	err := d.e.Store.View(ctx, d.e.Repository, func(r store.Reader) error {
		var err error
		evidence, err = runEvidence(r, d.run, d.plan)
		return err
	})
	if err != nil {
		return d.run, err
	}
	var failed, incomplete []string
	for _, target := range evidence.Targets {
		for i, result := range target.Outcomes {
			if Excluded(d.plan, target.Target, d.plan.Environments[i]) {
				continue
			}
			name := string(target.Target.ID)
			switch result.Outcome {
			case model.OutcomePassed:
			case model.OutcomeFailed, model.OutcomeBlocked:
				if !slices.Contains(failed, name) {
					failed = append(failed, name)
				}
			case model.OutcomeUnmet:
				unmet, _ := d.plan.UnmetIn(d.plan.Environments[i], target.Target.ID)
				problem := fmt.Sprintf("%s isn't built on %s: it %s", name, describeEnvironment(d.plan.Environments[i]), UnmetWords(unmet))
				if remedy := d.e.Remedy(unmet); remedy != "" {
					problem += "; " + remedy
				}
				d.problems = append(d.problems, problem)
			default:
				if !slices.Contains(incomplete, name) {
					incomplete = append(incomplete, name)
				}
			}
		}
	}
	state, detail := model.RunPassed, "passed"
	switch {
	case len(failed) > 0:
		state, detail = model.RunFailed, strings.Join(failed, ", ")+" did not pass"
	case len(d.problems) > 0 || len(incomplete) > 0:
		state = model.RunAttention
		if len(incomplete) > 0 {
			d.problems = append(d.problems, strings.Join(incomplete, ", ")+" did not finish")
		}
		detail = strings.Join(d.problems, "; ")
	}
	err = d.fenced(ctx, func(tx store.Tx) error {
		now := d.e.now()
		d.run.State, d.run.FinishedAt, d.run.Detail = state, &now, detail
		if err := tx.UpdateRun(d.run); err != nil {
			return err
		}
		_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: "run.state", Level: model.LevelInfo, Message: fmt.Sprintf("%s %s: %s", d.run.Name(), state, detail)})
		return err
	})
	return d.run, err
}

// build is what a provider records through.
type build struct {
	d         *driver
	ctx       context.Context
	execution model.GuestExecution
	// tree is the revision's, which the build reads.
	tree    model.ObjectID
	results map[model.TargetID]model.TargetResult
	// inputs are what each target's build read, as the provider reported
	// them (Consumed), until its result is recorded.
	inputs map[model.TargetID]model.TargetInputs
	// installs are the kept archives the guest installs targets from.
	installs []buildenv.Archive
}

func (b *build) Canceled() bool { return b.ctx.Err() != nil && !b.d.stopped() }

func (b *build) Blocked(target model.TargetID) (model.TargetID, bool) {
	if _, ok := b.d.plan.Target(target); !ok {
		return "", false
	}
	for _, dep := range b.d.plan.DependsOnIn(b.execution.Environment, target) {
		if result, ok := b.results[dep]; ok && result.Outcome != model.OutcomePassed {
			return dep, true
		}
	}
	return "", false
}

// Consumed completes what the provider saw with the revision's trees
// (reuse.Inputs). Inputs that can't be read are left unknown, and a
// result stands without them.
func (b *build) Consumed(target model.TargetID, active []model.ActivePort) {
	planned, ok := b.d.plan.Target(target)
	if !ok {
		return
	}
	// A port the guest was to install from its kept archive, active from
	// another, is one MacPorts got otherwise: said, since the build read
	// that one.
	for _, port := range active {
		for _, archive := range b.installs {
			if strings.EqualFold(port.Name, archive.Port) && port.Archive != "" && port.Archive != archive.Digest {
				b.Progress(fmt.Sprintf("%s built with %s from another archive than the one kept of its build: MacPorts chose %s", target, port.Name, port.Archive))
			}
		}
	}
	inputs, err := reuse.Inputs(b.ctx, b.d.e.Repo, b.tree, b.execution.Identity, planned, active)
	if err != nil {
		b.Progress(fmt.Sprintf("%s: what its build read wasn't recorded: %v", target, err))
		return
	}
	b.inputs[target] = inputs
}

func (b *build) Record(result model.TargetResult) error {
	result = b.d.plan.Tests.Judge(result)
	result.Execution = b.execution.ID
	if result.RecordedAt.IsZero() {
		result.RecordedAt = b.d.e.now()
	}
	inputs, read := b.inputs[result.Target]
	err := b.d.fenced(b.ctx, func(tx store.Tx) error {
		if read {
			key, err := tx.RecordInputs(inputs)
			if err != nil {
				return err
			}
			result.Inputs = key
		}
		if err := tx.RecordResult(result); err != nil {
			return err
		}
		message := fmt.Sprintf("%s: %s %s", describeEnvironment(b.execution.Environment), result.Target, result.Outcome)
		if result.Phase != "" {
			message += " at " + string(result.Phase)
		}
		_, err := b.d.session.Emit(tx, model.Event{Branch: b.d.run.Branch, Run: b.d.run.ID, Target: result.Target, Kind: "target.result", Level: model.LevelInfo, Message: message})
		return err
	})
	if err == nil {
		b.results[result.Target] = result
		delete(b.inputs, result.Target)
	}
	return err
}

func (b *build) Refer(ref string) error {
	b.execution.ProviderRef = ref
	return b.d.fenced(b.ctx, func(tx store.Tx) error { return tx.UpdateExecution(b.execution) })
}

func (b *build) Observe(observed model.Observed) error {
	b.execution.Observed = observed
	return b.d.fenced(b.ctx, func(tx store.Tx) error { return tx.UpdateExecution(b.execution) })
}

func (b *build) Progress(message string) {
	b.d.emit(b.ctx, "progress", describeEnvironment(b.execution.Environment)+": "+message)
}

// blockRemaining records as blocked the targets a provider left without a
// result whose changed dependency did not pass.
func (b *build) blockRemaining(targets []buildenv.Target) {
	for _, target := range targets {
		if _, ok := b.results[target.ID]; ok {
			continue
		}
		if _, blocked := b.Blocked(target.ID); blocked {
			_ = b.Record(model.TargetResult{Target: target.ID, Outcome: model.OutcomeBlocked})
		}
	}
}

// mergedResults are a run's results in one environment across its
// attempts: a later attempt's result replaces an earlier one's unless the
// earlier one is a complete verdict, which is final.
func mergedResults(r store.Reader, executions []model.GuestExecution) (map[model.TargetID]model.TargetResult, error) {
	merged := map[model.TargetID]model.TargetResult{}
	slices.SortFunc(executions, func(a, b model.GuestExecution) int { return a.Attempt - b.Attempt })
	for _, execution := range executions {
		results, err := r.Results(execution.ID)
		if err != nil {
			return nil, err
		}
		for _, result := range results {
			if earlier, ok := merged[result.Target]; ok && earlier.Outcome.Complete() {
				continue
			}
			merged[result.Target] = result
		}
	}
	return merged, nil
}

// RunEvidence is what a run established for each target in each of its
// environments.
func (e *Engine) RunEvidence(ctx context.Context, id model.RunID) (Evidence, error) {
	var evidence Evidence
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		run, err := r.Run(id)
		if err != nil {
			return err
		}
		plan, err := r.Plan(run.Plan)
		if err != nil {
			return err
		}
		evidence, err = runEvidence(r, run, plan)
		return err
	})
	return evidence, err
}

// runEvidence is what a run established for each target in each of its
// environments.
func runEvidence(r store.Reader, run model.Run, plan model.Plan) (Evidence, error) {
	executions, err := r.Executions(run.ID)
	if err != nil {
		return Evidence{}, err
	}
	byEnvironment := map[model.Environment][]model.GuestExecution{}
	for _, execution := range executions {
		byEnvironment[execution.Environment] = append(byEnvironment[execution.Environment], execution)
	}
	evidence := Evidence{Run: run, Plan: plan, Executions: map[model.ExecutionID]model.GuestExecution{}, policies: map[model.RunID]model.TestPolicy{run.ID: plan.Tests}}
	for _, execution := range executions {
		evidence.Executions[execution.ID] = execution
	}
	merged := map[model.Environment]map[model.TargetID]model.TargetResult{}
	for environment, list := range byEnvironment {
		if merged[environment], err = mergedResults(r, list); err != nil {
			return Evidence{}, err
		}
	}
	for _, target := range plan.Targets {
		te := TargetEvidence{Target: target, Passed: true}
		for _, environment := range plan.Environments {
			if Excluded(plan, target, environment) {
				te.Outcomes = append(te.Outcomes, model.TargetResult{Target: target.ID, Outcome: model.OutcomeNotRun})
				continue
			}
			if _, unmet := plan.UnmetIn(environment, target.ID); unmet {
				te.Outcomes = append(te.Outcomes, model.TargetResult{Target: target.ID, Outcome: model.OutcomeUnmet})
				te.Passed = false
				continue
			}
			result, ok := merged[environment][target.ID]
			if !ok {
				result = model.TargetResult{Target: target.ID, Outcome: model.OutcomeNotRun}
			}
			te.Outcomes = append(te.Outcomes, result)
			te.Passed = te.Passed && result.Outcome == model.OutcomePassed
			if result.ReusedFrom != "" {
				if err := evidence.origin(r, result.ReusedFrom); err != nil {
					return Evidence{}, err
				}
			}
		}
		evidence.Targets = append(evidence.Targets, te)
	}
	return evidence, nil
}

// revisionCommit is a commit holding the revision's files: the commit
// itself, or for a snapshot one made of its tree on the base, kept under
// refs/dockhand/revisions so it is not collected while checks use it.
func (e *Engine) revisionCommit(ctx context.Context, revision model.Revision) (string, error) {
	if revision.Kind == model.RevisionCommit {
		return string(revision.Source.Commit), nil
	}
	ref := "refs/dockhand/revisions/" + string(revision.ID)
	if existing, err := e.Repo.ReadRef(ctx, ref); err != nil {
		return "", err
	} else if existing.Exists {
		return existing.Object, nil
	}
	who := git.Signature{Name: "dockhand", Email: "dockhand@localhost", When: revision.CreatedAt}
	commit, err := e.Repo.WriteCommit(ctx, git.Commit{Tree: string(revision.Source.Tree), Parents: []string{string(revision.Source.Base)},
		Message: fmt.Sprintf("dockhand %s\n", Describe(revision)), Author: who, Committer: who})
	if err != nil {
		return "", err
	}
	if err := e.Repo.UpdateRefs(ctx, []git.RefChange{{Name: ref, Desired: git.RefValue{Exists: true, Object: commit}}}); err != nil {
		var conflict *git.RefConflict
		if !errors.As(err, &conflict) {
			return "", err
		}
	}
	return commit, nil
}

// LogDirectory is where check logs are kept: logs beside the database.
func (e *Engine) LogDirectory() string {
	return filepath.Join(filepath.Dir(e.options.Database), "logs")
}

func describeEnvironment(environment model.Environment) string {
	return DescribeEnvironment(environment)
}

// DescribeEnvironment words where a check builds for a person: the provider,
// and the release it builds on by its macOS name, "tart macOS 26 (Tahoe)
// arm64", or its raw platform where the release is unknown, with its
// developer tools where the provider states them.
func DescribeEnvironment(environment model.Environment) string {
	switch environment.DeveloperTools {
	case model.DeveloperToolsXcode:
		return describePlace(environment) + " with Xcode"
	case model.DeveloperToolsCommandLine:
		return describePlace(environment) + " with the Command Line Tools"
	}
	return describePlace(environment)
}

// EnvironmentHeading is an environment's short name, for a column heading
// where its full words are shown elsewhere: its release alone, macOS 26,
// with its architecture where another environment of all shares the
// release, and its provider where all has several. An environment with no
// platform is its provider.
func EnvironmentHeading(environment model.Environment, all []model.Environment) string {
	platform := environment.Platform
	if platform.Version == "" && platform.Architecture == "" {
		return environment.Provider
	}
	var parts []string
	providers := map[string]bool{}
	shared := 0
	for _, other := range all {
		providers[other.Provider] = true
		if other.Provider == environment.Provider && other.Platform.OS == platform.OS && other.Platform.Version == platform.Version {
			shared++
		}
	}
	if len(providers) > 1 {
		parts = append(parts, environment.Provider)
	}
	parts = append(parts, releaseWords(platform))
	if shared > 1 && platform.Architecture != "" {
		parts = append(parts, platform.Architecture)
	}
	return strings.Join(parts, " ")
}

// releaseWords name a platform's release: macOS 26, or Darwin 30 where the
// release is unknown, whose Darwin version isn't macOS's.
func releaseWords(platform model.Platform) string {
	if platform.OS == "darwin" || platform.OS == "" {
		if darwin, err := strconv.Atoi(platform.Version); err == nil {
			if release, err := macos.ReleaseForDarwin(darwin); err == nil {
				return "macOS " + release.Product
			}
		}
		return "Darwin " + platform.Version
	}
	return strings.TrimSpace(platform.OS + " " + platform.Version)
}

func describePlace(environment model.Environment) string {
	platform := environment.Platform
	if platform.Version == "" && platform.Architecture == "" {
		return environment.Provider
	}
	if platform.OS == "" {
		platform.OS = "darwin"
	}
	// A release macos doesn't know keeps its Darwin version, which isn't
	// macOS's: Darwin 25 is macOS 26.
	described := macos.Describe(platform)
	if rest, ok := strings.CutPrefix(described, "darwin "); ok {
		described = "Darwin " + rest
	}
	return environment.Provider + " " + described
}

// environmentSlug names an environment's log directory: its provider, its
// release as everything else names it, macOS 26 as macos26, and its
// architecture. A Darwin release macos doesn't know keeps its Darwin
// version, darwin30, which isn't macOS's.
func environmentSlug(environment model.Environment) string {
	parts := []string{environment.Provider}
	platform := environment.Platform
	if platform.Version != "" {
		release := platform.OS + platform.Version
		if platform.OS == "darwin" || platform.OS == "" {
			release = "darwin" + platform.Version
			if darwin, err := strconv.Atoi(platform.Version); err == nil {
				if known, err := macos.ReleaseForDarwin(darwin); err == nil {
					release = "macos" + known.Product
				}
			}
		}
		parts = append(parts, release)
	}
	if platform.Architecture != "" {
		parts = append(parts, platform.Architecture)
	}
	return strings.Join(parts, "-")
}

// Run reads a run.
func (e *Engine) Run(ctx context.Context, id model.RunID) (model.Run, error) {
	var run model.Run
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		run, err = r.Run(id)
		return err
	})
	return run, err
}

// Revision reads a revision.
func (e *Engine) Revision(ctx context.Context, id model.RevisionID) (model.Revision, error) {
	var revision model.Revision
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		revision, err = r.Revision(id)
		return err
	})
	return revision, err
}

// Plan reads a verification plan.
func (e *Engine) Plan(ctx context.Context, id model.PlanID) (model.Plan, error) {
	var plan model.Plan
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		plan, err = r.Plan(id)
		return err
	})
	return plan, err
}

// Events reads the journal past a sequence number, oldest first: what
// observers tail (Design v3 §11).
func (e *Engine) Events(ctx context.Context, after int64) ([]model.Event, error) {
	var events []model.Event
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		for {
			batch, err := r.Events(after, 500)
			if err != nil || len(batch) == 0 {
				return err
			}
			events = append(events, batch...)
			after = batch[len(batch)-1].Sequence
		}
	})
	return events, err
}

// RunEvents are one run's events after a sequence, oldest first: what a
// command following the run shows, read by the run rather than through the
// whole journal.
func (e *Engine) RunEvents(ctx context.Context, run model.RunID, after int64) ([]model.Event, error) {
	var events []model.Event
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		for {
			batch, err := r.RunEvents(run, after, 500)
			if err != nil || len(batch) == 0 {
				return err
			}
			events = append(events, batch...)
			after = batch[len(batch)-1].Sequence
		}
	})
	return events, err
}

// LatestEvent is the journal's newest sequence, where one watching what
// happens from now starts.
func (e *Engine) LatestEvent(ctx context.Context) (int64, error) {
	var last int64
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		last, err = r.LastEvent()
		return err
	})
	return last, err
}

// Candidates are the runs serve could drive, in the order it takes them:
// a run left running by a driver that is gone, then queued runs a person
// asked for, then serve's own, oldest first. Runs a live session holds,
// this one included, are left out.
func (e *Engine) Candidates(ctx context.Context, session *coord.Session) ([]model.Run, error) {
	runs, err := e.Runs(ctx, store.RunFilter{States: []model.RunState{model.RunQueued, model.RunRunning}})
	if err != nil {
		return nil, err
	}
	rank := func(run model.Run) int {
		switch {
		case run.State == model.RunRunning:
			return 0
		case run.Origin == model.OriginPerson:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(runs, func(a, b model.Run) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return a.Number - b.Number
	})
	var candidates []model.Run
	for _, run := range runs {
		holder, err := session.Holder(ctx, RunResource(run.ID))
		if err != nil {
			return nil, err
		}
		if holder == nil {
			candidates = append(candidates, run)
		}
	}
	return candidates, nil
}

// RunProviders are the providers a run's plan builds on, each once.
func (e *Engine) RunProviders(ctx context.Context, run model.Run) ([]string, error) {
	var providers []string
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		plan, err := r.Plan(run.Plan)
		if err != nil {
			return err
		}
		for _, environment := range plan.Environments {
			if !slices.Contains(providers, environment.Provider) {
				providers = append(providers, environment.Provider)
			}
		}
		return nil
	})
	return providers, err
}
