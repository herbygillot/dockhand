package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// RunResource is the lease a run's driver holds.
func RunResource(id model.RunID) string { return "run:" + string(id) }

// Enqueue records a check request: its plan and a queued run. The request
// is immutable from here; editing the branch never changes what it means.
func (e *Engine) Enqueue(ctx context.Context, branch model.Branch, plan model.Plan, origin model.Origin) (model.Run, error) {
	var run model.Run
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		if err := tx.AddPlan(plan); err != nil {
			return err
		}
		number, err := tx.NextRunNumber()
		if err != nil {
			return err
		}
		run = model.Run{ID: model.RunID(store.NewID("run")), Branch: branch.ID, Revision: plan.Revision, Plan: plan.ID, Number: number, Origin: origin, State: model.RunQueued, CreatedAt: e.now()}
		if err := tx.AddRun(run); err != nil {
			return err
		}
		_, err = tx.AppendEvent(model.Event{At: run.CreatedAt, Branch: branch.ID, Run: run.ID, Kind: "run.state", Level: model.LevelInfo,
			Message: fmt.Sprintf("%s queued for %s", run.Name(), branch.ShortName())})
		return err
	})
	return run, err
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
	// problems are why the run needs a person's attention.
	problems []string
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
	for _, environment := range d.plan.Environments {
		if running.Err() != nil {
			break
		}
		provider, ok := e.Providers[environment.Provider]
		if !ok {
			d.problems = append(d.problems, fmt.Sprintf("no provider %q is set up here", environment.Provider))
			continue
		}
		if err := d.environment(running, provider, environment, revision, commit); err != nil {
			return d.run, err
		}
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
func (d *driver) environment(ctx context.Context, provider Provider, environment model.Environment, revision model.Revision, commit string) error {
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
		var remaining []model.PlanTarget
		for _, target := range d.plan.Targets {
			if Excluded(d.plan, target, environment.Platform) {
				continue
			}
			if result, ok := results[target.ID]; !ok || !result.Outcome.Complete() {
				remaining = append(remaining, target)
			}
		}
		if len(remaining) == 0 {
			return nil
		}
		if attempt >= model.MaxAttempts {
			d.problems = append(d.problems, fmt.Sprintf("%s failed %d times for reasons of its own; see dockhand logs %s", describeEnvironment(environment), attempt, d.run.Name()))
			return nil
		}
		execution := model.GuestExecution{ID: model.ExecutionID(store.NewID("ex")), Run: d.run.ID, Environment: environment, Attempt: attempt + 1, State: model.ExecutionWaiting, CreatedAt: e.now()}
		if err := d.fenced(ctx, func(tx store.Tx) error {
			if err := tx.AddExecution(execution); err != nil {
				return err
			}
			execution.State = model.ExecutionRunning
			if err := tx.UpdateExecution(execution); err != nil {
				return err
			}
			_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: "execution.state", Level: model.LevelInfo,
				Message: fmt.Sprintf("%s: attempt %d of %d on %s", d.run.Name(), execution.Attempt, model.MaxAttempts, describeEnvironment(environment))})
			return err
		}); err != nil {
			return err
		}
		build := &build{d: d, ctx: ctx, execution: execution, results: results}
		job := Job{Run: d.run, Execution: execution, Revision: revision, Plan: d.plan, Environment: environment, Targets: remaining, Commit: commit,
			Directory: filepath.Join(e.LogDirectory(), d.run.Name(), fmt.Sprintf("%s-%d", environmentSlug(environment), execution.Attempt))}
		err := provider.Execute(ctx, job, build)
		build.blockRemaining(remaining)
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
			if Excluded(d.plan, target.Target, d.plan.Environments[i].Platform) {
				continue
			}
			name := string(target.Target.ID)
			switch result.Outcome {
			case model.OutcomePassed:
			case model.OutcomeFailed, model.OutcomeBlocked:
				if !slices.Contains(failed, name) {
					failed = append(failed, name)
				}
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
	results   map[model.TargetID]model.TargetResult
}

func (b *build) Blocked(target model.TargetID) (model.TargetID, bool) {
	planned, ok := b.d.plan.Target(target)
	if !ok {
		return "", false
	}
	for _, dep := range planned.DependsOn {
		if result, ok := b.results[dep]; ok && result.Outcome != model.OutcomePassed {
			return dep, true
		}
	}
	return "", false
}

func (b *build) Record(result model.TargetResult) error {
	result.Execution = b.execution.ID
	if result.RecordedAt.IsZero() {
		result.RecordedAt = b.d.e.now()
	}
	err := b.d.fenced(b.ctx, func(tx store.Tx) error {
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
	}
	return err
}

func (b *build) Progress(message string) {
	b.d.emit(b.ctx, "progress", describeEnvironment(b.execution.Environment)+": "+message)
}

// blockRemaining records as blocked the targets a provider left without a
// result whose changed dependency did not pass.
func (b *build) blockRemaining(targets []model.PlanTarget) {
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
	evidence := Evidence{Run: run, Plan: plan}
	merged := map[model.Environment]map[model.TargetID]model.TargetResult{}
	for environment, list := range byEnvironment {
		if merged[environment], err = mergedResults(r, list); err != nil {
			return Evidence{}, err
		}
	}
	for _, target := range plan.Targets {
		te := TargetEvidence{Target: target, Passed: true}
		for _, environment := range plan.Environments {
			if Excluded(plan, target, environment.Platform) {
				te.Outcomes = append(te.Outcomes, model.TargetResult{Target: target.ID, Outcome: model.OutcomeNotRun})
				continue
			}
			result, ok := merged[environment][target.ID]
			if !ok {
				result = model.TargetResult{Target: target.ID, Outcome: model.OutcomeNotRun}
			}
			te.Outcomes = append(te.Outcomes, result)
			te.Passed = te.Passed && result.Outcome == model.OutcomePassed
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
	parts := []string{environment.Provider}
	platform := environment.Platform
	if platform.Version != "" {
		parts = append(parts, platformName(platform.OS)+" "+platform.Version)
	}
	if platform.Architecture != "" {
		parts = append(parts, platform.Architecture)
	}
	return strings.Join(parts, " ")
}

func environmentSlug(environment model.Environment) string {
	parts := []string{environment.Provider}
	for _, part := range []string{environment.Platform.Version, environment.Platform.Architecture} {
		if part != "" {
			parts = append(parts, part)
		}
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

// Next is the run serve drives next (Design v3 §11): a run left running by
// a driver that is gone, then queued runs a person asked for, then serve's
// own, oldest first. Runs a live session holds are someone else's.
func (e *Engine) Next(ctx context.Context, session *coord.Session) (model.Run, bool, error) {
	runs, err := e.Runs(ctx, store.RunFilter{States: []model.RunState{model.RunQueued, model.RunRunning}})
	if err != nil {
		return model.Run{}, false, err
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
	for _, run := range runs {
		holder, err := session.Holder(ctx, RunResource(run.ID))
		if err != nil {
			return model.Run{}, false, err
		}
		if holder == nil {
			return run, true, nil
		}
	}
	return model.Run{}, false, nil
}
