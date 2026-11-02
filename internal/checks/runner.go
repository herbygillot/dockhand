package checks

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/failpoint"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/reuse"
	"github.com/herbygillot/dockhand/internal/store"
)

// RunResource is the lease a run's driver holds.
func RunResource(id model.RunID) string { return "run:" + string(id) }

// Enqueue records a check request: its plan and a queued run. The request
// is immutable from here; editing the branch never changes what it means.
// A branch has one check at a time (decision 29), so it refuses while
// another is queued or running (ActiveRunError).
func (c *Service) Enqueue(ctx context.Context, branch model.Branch, plan model.Plan, origin model.Origin) (model.Run, error) {
	return c.enqueue(ctx, branch, plan, origin, "")
}

// ActiveCheck is the branch's one check queued or running, beside which a
// new one of revision would stand: nil where there's none, and with it
// every run that holds the branch, which replacing it stops. A baseline
// looks beside the check it explains, and a run asked to stop is on its way
// out: neither is the branch's one check. Stopped is judged with session.
func (c *Service) ActiveCheck(ctx context.Context, session *coord.Session, branch model.Branch, revision model.Revision) (*ActiveRunError, []model.Run, error) {
	var runs []model.Run
	var active *ActiveRunError
	if err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
		var err error
		if runs, err = r.Runs(store.RunFilter{Branch: branch.ID, States: []model.RunState{model.RunQueued, model.RunRunning}}); err != nil {
			return err
		}
		runs = slices.DeleteFunc(runs, func(run model.Run) bool { return !run.BranchCheck() })
		if len(runs) == 0 {
			return nil
		}
		active = &ActiveRunError{Run: runs[0], SameRevision: runs[0].Revision == revision.ID}
		active.Checking, err = r.Revision(runs[0].Revision)
		return err
	}); err != nil || active == nil {
		return nil, nil, err
	}
	var err error
	if active.Stopped, err = Stopped(ctx, session, runs[0]); err != nil {
		return nil, nil, err
	}
	return active, runs, nil
}

// Replace stops the runs a new check replaces, keeping what they finished:
// a queued one canceled at once, a running one asked to stop, which its
// process does at its next step. It returns each as it is after
// (Enqueue(Replace), the command package review's row 7).
func (c *Service) Replace(ctx context.Context, session *coord.Session, runs []model.Run) ([]model.Run, error) {
	var replaced []model.Run
	for _, run := range runs {
		after, err := c.RequestCancel(ctx, session, run.ID)
		if err != nil {
			return replaced, err
		}
		replaced = append(replaced, after)
	}
	return replaced, nil
}

// EnqueueBaseline records a baseline's request, a run marked as looking
// into the check of, beside the branch's one check (queue).
func (c *Service) EnqueueBaseline(ctx context.Context, branch model.Branch, plan model.Plan, origin model.Origin, of model.RunID) (model.Run, error) {
	return c.enqueue(ctx, branch, plan, origin, of)
}

func (c *Service) enqueue(ctx context.Context, branch model.Branch, plan model.Plan, origin model.Origin, baselineOf model.RunID) (model.Run, error) {
	var run model.Run
	err := c.Store.Update(ctx, c.Repository, func(tx store.Tx) error {
		if err := tx.AddPlan(plan); err != nil {
			return err
		}
		var err error
		run, err = c.queue(tx, model.Run{Branch: branch.ID, Revision: plan.Revision, Plan: plan.ID, Origin: origin, BaselineOf: baselineOf}, "queued for "+branch.ShortName())
		return err
	})
	return run, err
}

// queue records run as queued, numbered and stamped now, saying why in the
// journal. A branch has one check at a time (decision 29): a run that isn't
// a baseline, which looks beside the check it explains, is refused while
// another of the branch's is queued or running and not asked to stop.
func (c *Service) queue(tx store.Tx, run model.Run, why string) (model.Run, error) {
	if run.BranchCheck() {
		active, err := tx.Runs(store.RunFilter{Branch: run.Branch, States: []model.RunState{model.RunQueued, model.RunRunning}})
		if err != nil {
			return run, err
		}
		for _, other := range active {
			if !other.BranchCheck() {
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
	run.ID, run.Number, run.State, run.CreatedAt = model.RunID(store.NewID("run")), number, model.RunQueued, c.Now()
	if err := tx.AddRun(run); err != nil {
		return run, err
	}
	_, err = tx.AppendEvent(model.Event{At: run.CreatedAt, Branch: run.Branch, Run: run.ID, Kind: model.EventRunState, Level: model.LevelInfo,
		Message: run.Name() + " " + why})
	return run, err
}

// Retry queues a run's exact request again: the same revision and plan,
// whatever the branch holds now, as the branch's one check (queue). Until
// per-target reuse lands, the whole plan is built again.
func (c *Service) Retry(ctx context.Context, previous model.Run) (model.Run, error) {
	if !previous.State.Terminal() {
		return model.Run{}, fmt.Errorf("%s is still %s; dockhand wait %s follows it", previous.Name(), previous.State, previous.Name())
	}
	var run model.Run
	err := c.Store.Update(ctx, c.Repository, func(tx store.Tx) error {
		var err error
		run, err = c.queue(tx, model.Run{Branch: previous.Branch, Revision: previous.Revision, Plan: previous.Plan, Origin: model.OriginPerson, BaselineOf: previous.BaselineOf}, "queued, retrying "+previous.Name())
		return err
	})
	return run, err
}

// ActiveRunError is the branch's check already queued or running, which a
// new one for the branch would stand beside: a branch has one check at a
// time (decision 29).
type ActiveRunError struct {
	Run model.Run
	// Stopped is a run recorded as running whose process ended, which
	// dockhand wait resumes (ActiveCheck judges it).
	Stopped bool
	// Checking is what it checks, and SameRevision whether that is what
	// the new check would have.
	Checking     model.Revision
	SameRevision bool
	// Uncovered is a run of the same files whose plan doesn't cover the
	// new check's (PlanCovers), which a follow refuses.
	Uncovered bool
}

func (a *ActiveRunError) Error() string {
	name := a.Run.Name()
	if a.Stopped && a.SameRevision {
		return fmt.Sprintf("%s stopped before it finished these files, when the process running it ended; dockhand wait %s resumes it", name, name)
	}
	if a.Uncovered {
		return fmt.Sprintf("%s is %s for these files, but doesn't cover what this check asks, the builds it makes in each environment, its variants, tests, or freshness, or a kept guest. Run dockhand cancel %s and run this again, or wait for it to finish and run this again", name, a.Run.State, name)
	}
	if a.SameRevision {
		return fmt.Sprintf("%s is already %s for these files; dockhand wait %s follows it", name, a.Run.State, name)
	}
	return fmt.Sprintf("%s is %s for %s, and a branch has one check at a time; dockhand cancel %s stops it, keeping what it finished", name, a.Run.State, Describe(a.Checking), name)
}

// RequestCancel records that a run should stop. A queued run is canceled
// at once; a running one is stopped by whoever holds it, or by this
// session when no one does.
func (c *Service) RequestCancel(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	var run model.Run
	err := c.Store.Update(ctx, c.Repository, func(tx store.Tx) error {
		var err error
		if run, err = tx.Run(id); err != nil {
			return err
		}
		if run.State.Terminal() || run.CancelRequested != nil {
			return nil
		}
		now := c.Now()
		run.CancelRequested = &now
		if run.State == model.RunQueued {
			run.State, run.FinishedAt, run.Detail = model.RunCanceled, &now, "canceled before it started"
		}
		if err := tx.UpdateRun(run); err != nil {
			return err
		}
		_, err = tx.AppendEvent(model.Event{At: now, Session: session.ID(), Branch: run.Branch, Run: run.ID, Kind: model.EventRunCancel, Level: model.LevelInfo, Message: run.Name() + ": cancel requested"})
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
	return c.finishCanceled(ctx, session, lease, id)
}

func (c *Service) finishCanceled(ctx context.Context, session *coord.Session, lease model.Lease, id model.RunID) (model.Run, error) {
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
		now := c.Now()
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
		_, err = session.Emit(tx, model.Event{Branch: run.Branch, Run: run.ID, Kind: model.EventRunState, Level: model.LevelInfo, Message: run.Name() + " canceled"})
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
func (c *Service) Drive(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	return c.drive(ctx, session, id, false)
}

// Resume drives a run as Drive does, except that when ctx ends without a
// cancel request, the run is left running for the next driver: serve
// stopping, or its Mac restarting, loses nothing (Design v3 §11). The
// execution it interrupted counts as one attempt.
func (c *Service) Resume(ctx context.Context, session *coord.Session, id model.RunID) (model.Run, error) {
	return c.drive(ctx, session, id, true)
}

func (c *Service) drive(ctx context.Context, session *coord.Session, id model.RunID, resumable bool) (model.Run, error) {
	var lease model.Lease
	err := c.Store.Update(ctx, c.Repository, func(tx store.Tx) error {
		var err error
		lease, err = c.admit(tx, session, id)
		return err
	})
	if err != nil {
		return model.Run{}, err
	}
	return c.DriveLeased(ctx, session, lease, id, resumable)
}

// DriveLeased drives a run whose lease the session took, as its slots
// (Reserve), and releases it when the run's driving ends.
func (c *Service) DriveLeased(ctx context.Context, session *coord.Session, lease model.Lease, id model.RunID, resumable bool) (model.Run, error) {
	defer session.Release(context.WithoutCancel(ctx), lease)
	d := &driver{c: c, session: session, lease: lease, resumable: resumable}
	return d.drive(ctx, id)
}

type driver struct {
	c       *Service
	session *coord.Session
	lease   model.Lease
	run     model.Run
	plan    model.Plan
	// resumable leaves a run whose driver stopped running, for the next.
	resumable bool
	// canceled records that a cancel request, not the driver stopping,
	// ended the run.
	canceled atomic.Bool
	// estimates are the environments' install estimates being said
	// (sayEstimate), which the run waits for before it finishes.
	estimates sync.WaitGroup
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

// anotherCanReuse reports whether another environment of the check has an
// identity, which reuse needs.
func (d *driver) anotherCanReuse(ctx context.Context, environment model.Environment) bool {
	for other, identity := range d.c.Identities(ctx, d.plan.Environments) {
		if other != environment && identity != "" {
			return true
		}
	}
	return false
}

func (d *driver) emit(ctx context.Context, kind model.EventKind, message string) {
	d.emitAt(ctx, kind, model.LevelInfo, message)
}

// emitAt journals an event of the run's at a level.
func (d *driver) emitAt(ctx context.Context, kind model.EventKind, level model.EventLevel, message string) {
	_ = d.fenced(ctx, func(tx store.Tx) error {
		_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: kind, Level: level, Message: message})
		return err
	})
}

func (d *driver) drive(ctx context.Context, id model.RunID) (model.Run, error) {
	c := d.c
	var revision model.Revision
	if err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
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
		return c.finishCanceled(ctx, d.session, d.lease, id)
	}
	if d.run.State == model.RunQueued {
		if err := d.fenced(ctx, func(tx store.Tx) error {
			d.run.State = model.RunRunning
			if err := tx.UpdateRun(d.run); err != nil {
				return err
			}
			_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: model.EventRunState, Level: model.LevelInfo, Message: d.run.Name() + " running"})
			return err
		}); err != nil {
			return d.run, err
		}
	}

	// A cancel recorded by another command stops this one at its next step.
	running, stop := context.WithCancel(ctx)
	defer stop()
	go d.watchCancel(running, stop)

	commit, err := c.revisionCommit(ctx, revision)
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
		if _, ok := c.Providers[environment.Provider]; !ok {
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
		provider, queue := c.Providers[name], queues[name]
		// A provider whose one run builds all its environments gets them
		// together (buildenv.MatrixProvider).
		if matrix, ok := provider.(buildenv.MatrixProvider); ok && matrix.Matrix() {
			group.Go(func() error { return d.matrix(together, provider, queue, revision, commit) })
			continue
		}
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
	err = group.Wait()
	d.estimates.Wait()
	if err != nil {
		return d.run, err
	}
	if running.Err() != nil {
		if d.stopped() {
			return c.run(context.WithoutCancel(ctx), id)
		}
		// An interrupt cancels ctx too; the run is still settled.
		return c.finishCanceled(context.WithoutCancel(ctx), d.session, d.lease, id)
	}
	// Beside the build, what upstream's change means for each port it
	// changes is collected where it isn't recorded, for the submission
	// that follows (the assessment design, D). It never fails the check:
	// what it couldn't do, its assessment says.
	if c.Hooks != nil {
		if err := c.Hooks.AfterBuild(ctx, d.run, revision); err != nil {
			d.emit(ctx, model.EventProgress, "upstream's change couldn't be assessed: "+err.Error())
		}
	}
	return d.finish(context.WithoutCancel(ctx))
}

// stopped reports that the driver itself is stopping, with no cancel
// request, in a driver that leaves such runs for the next one.
func (d *driver) stopped() bool { return d.resumable && !d.canceled.Load() }

// watchCancel polls the run for a cancel request.
func (d *driver) watchCancel(ctx context.Context, stop context.CancelFunc) {
	ticker := time.NewTicker(d.c.Poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = d.c.Store.View(ctx, d.c.Repository, func(r store.Reader) error {
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

// environment runs the guest executions one environment needs.
func (d *driver) environment(ctx context.Context, provider buildenv.Provider, environment model.Environment, revision model.Revision, commit string) error {
	c := d.c
	// What the work tells a person as it goes is the run's to keep, as its
	// provider's progress is: whoever follows the run sees it, not only the
	// terminal driving it, and staging the index a guest takes can run for
	// minutes. Keeping it goes through the context without the observer,
	// so it can't report to itself. Every report says which environment
	// it's about, the verbose ones too, since two stage at once.
	kept := ctx
	ctx = progress.Keep(progress.Within(ctx, DescribeEnvironment(environment)), func(update progress.Update) {
		if update.Level == progress.Info {
			d.emit(kept, model.EventProgress, update.Message)
		}
	})
	d.estimates.Go(func() { d.sayEstimate(kept, environment, revision.Source) })
	for {
		var executions []model.GuestExecution
		var results map[model.TargetID]model.TargetResult
		if err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
			// The check's records in one read (the SQL study's O2).
			check, err := r.Check(d.run.ID)
			if err != nil {
				return err
			}
			executions = slices.DeleteFunc(check.Executions, func(x model.GuestExecution) bool { return x.Environment != environment })
			slices.SortFunc(executions, func(a, b model.GuestExecution) int { return a.Attempt - b.Attempt })
			results = evidence.Merged(executions, check.Results)
			return nil
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
		remaining := d.remaining(environment, results)
		if len(remaining) == 0 {
			return nil
		}
		if attempt >= model.MaxAttempts {
			d.problem(fmt.Sprintf("%s failed %d times because of an environment error. See dockhand logs %s", DescribeEnvironment(environment), attempt, d.run.Name()))
			return nil
		}
		// The environment's identity is read once for the attempt. Reuse
		// compares earlier builds' with it, and the execution records it as
		// it is when the execution begins: evidence compares it with the
		// environment's identity whenever it is judged (Counts).
		identity, err := c.Identity(ctx, environment)
		if err != nil {
			// One that can't be read isn't recorded as none, which a
			// provider that can't say records, and which evidence would
			// read later as the environment made again (the
			// code-organization review, finding 39): the attempt fails as
			// the environment's, and the next reads it again.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := d.unreadIdentity(ctx, environment, attempt, err); err != nil {
				return err
			}
			continue
		}
		// Before the first attempt, the targets that would build as an
		// earlier build did reuse its result (decision 28). When every one
		// does, nothing is built.
		targets, paths, err := d.earlier(ctx, environment, remaining)
		if err != nil {
			return err
		}
		if attempt == 0 && !d.plan.Fresh {
			d.sayNotReusable(ctx, environment, targets)
			d.sayTagMoved(ctx, environment, targets)
		}
		var reused map[model.TargetID]reuse.Candidate
		if attempt == 0 && identity == "" && !d.plan.Fresh && d.anotherCanReuse(ctx, environment) {
			// Where another environment of the check can reuse its results,
			// this one's building everything reads as a fault unless it's
			// said: a GitHub run records neither what its runner is nor
			// what the build read (the gh rebase's finding 1).
			d.emit(ctx, model.EventExecutionState, fmt.Sprintf("%s: %s: builds every target, since its provider doesn't say what the environment is, so no earlier result can be reused there", d.run.Name(), DescribeEnvironment(environment)))
		}
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
		execution := model.GuestExecution{ID: model.ExecutionID(store.NewID(environment.Provider)), Run: d.run.ID, Environment: environment, Identity: identity, Attempt: attempt + 1, State: model.ExecutionWaiting, CreatedAt: c.Now()}
		if err := d.fenced(ctx, func(tx store.Tx) error {
			if err := tx.AddExecution(execution); err != nil {
				return err
			}
			execution.State = model.ExecutionRunning
			if err := tx.UpdateExecution(execution); err != nil {
				return err
			}
			_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: model.EventExecutionState, Level: model.LevelInfo,
				Message: fmt.Sprintf("%s: attempt %d of %d on %s, run %s", d.run.Name(), execution.Attempt, model.MaxAttempts, DescribeEnvironment(environment), execution.ID)})
			return err
		}); err != nil {
			return err
		}
		failpoint.Hit("check.running")
		build := &build{d: d, ctx: ctx, execution: execution, tree: revision.Source.Tree, results: results, inputs: map[model.TargetID]model.TargetInputs{}, fetched: map[model.TargetID]model.ObjectID{}}
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
		builds := slices.DeleteFunc(targets, func(target reuse.Target) bool {
			return !slices.ContainsFunc(building, func(b buildenv.Target) bool { return b.ID == target.ID })
		})
		build.installs, build.builtWith, err = d.installs(ctx, environment, builds, build.results)
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
		// And the ports they depend on, from the archives earlier guests
		// here installed them from, rather than build them again (batch
		// 90). A provider that can't install archives leaves them.
		if dependencies, groups := d.dependencyInstalls(ctx, environment, revision.Source, builds); len(dependencies) > 0 {
			var names []string
			for _, archive := range dependencies {
				if !slices.Contains(names, archive.Port) {
					names = append(names, archive.Port)
				}
			}
			// A count, where the names ran to about 230 (the gnome-voice
			// session's notes); each target's build says which it took.
			// A port can have two kept archives, its newest and the one
			// before (dependencyArchivesPerPort), each given: gnome-voice's
			// "up to 221 dependencies" were 223 archives, freetype's and
			// p5.34-uri's two each (check-484).
			build.Progress(offeredWords(len(names), len(dependencies)))
			build.installs = append(build.installs, dependencies...)
			if build.builtWith == nil {
				build.builtWith = builtWith{}
			}
			maps.Copy(build.builtWith, groups)
		}
		job := buildenv.Job{Run: d.run, Execution: execution, Revision: revision, Plan: d.plan, Environment: environment, Targets: building, Commit: commit, Installs: build.installs,
			Directory: filepath.Join(c.Logs, d.run.Name(), fmt.Sprintf("%s-%d", environmentSlug(environment), execution.Attempt))}
		stopWatching := watchStalls(ctx, build, job.Directory)
		err = provider.Execute(ctx, job, build)
		stopWatching()
		build.blockRemaining(building)
		// An attempt that left no log keeps no directory for it: exiv2's
		// check-448 had an empty tart-macos27-arm64-1/ beside -2/, and
		// nothing said why (the exiv2 field notes, 2026-10-19). Remove
		// takes a directory only where it's empty.
		_ = os.Remove(job.Directory)
		// The build's copy holds what the provider reported.
		execution = build.execution
		switch {
		case ctx.Err() != nil && d.stopped():
			return d.endExecution(ctx, execution, model.ExecutionInfrastructure, "interrupted: the process driving it stopped")
		case ctx.Err() != nil:
			return d.endExecution(ctx, execution, model.ExecutionCanceled, "canceled")
		case errors.Is(err, buildenv.ErrNeedsAttention):
			// Another attempt won't fix it, as a guest refusing dockhand's
			// login won't: it's said, and no attempt is spent on it.
			if err := d.endExecution(ctx, execution, model.ExecutionInfrastructure, err.Error()); err != nil {
				return err
			}
			d.problem(fmt.Sprintf("%s: %v; another attempt won't fix it, so none was made", DescribeEnvironment(environment), err))
			return nil
		case errors.Is(err, buildenv.ErrDockhandFault):
			// A fault in dockhand's own handling, as a guest program
			// reporting what this dockhand can't read, would only repeat:
			// it's said at once, as dockhand's, where it cloned a VM for
			// each of three attempts (the architecture review's X2).
			if err := d.endExecution(ctx, execution, model.ExecutionInfrastructure, err.Error()); err != nil {
				return err
			}
			fault := refusal.WordsOf(err)
			d.problem(fmt.Sprintf("dockhand: %s: %s; the fault is dockhand's own, so another attempt would repeat it, and none was made", DescribeEnvironment(environment), fault))
			return nil
		case refusal.Transient(err):
			// What another try may meet, a VM or a runner that failed, or
			// the network upstream, is tried again: the one reading of it
			// (refusal.Transient, R3-6).
			d.emit(ctx, model.EventExecutionRetry, fmt.Sprintf("%s: %v", DescribeEnvironment(environment), err))
			if err := d.endExecution(ctx, execution, model.ExecutionInfrastructure, err.Error()); err != nil {
				return err
			}
			continue
		case err != nil:
			// What a provider didn't mark as infrastructure isn't a VM's
			// flake, as a platform this dockhand doesn't know isn't:
			// another attempt would meet it again, so it's said at once,
			// as dockhand's, where three attempts cloned three VMs for it
			// (the architecture review's X2, finished).
			if err := d.endExecution(ctx, execution, model.ExecutionInfrastructure, err.Error()); err != nil {
				return err
			}
			d.problem(fmt.Sprintf("dockhand: %s: %v; the provider didn't mark it as trouble another attempt could clear, so none was made", DescribeEnvironment(environment), err))
			return nil
		}
		return d.endExecution(ctx, execution, model.ExecutionFinished, "")
	}
}

// estimateWait bounds an environment's install estimate, which the run
// waits for before it finishes.
const estimateWait = 30 * time.Second

// sayEstimate says what the check will install in an environment, by the
// port index, once packages.macports.org's listings are had: beside the
// clone's start, never delaying it (the install estimate, 2026-10-29).
func (d *driver) sayEstimate(ctx context.Context, environment model.Environment, source model.Source) {
	ctx, cancel := context.WithTimeout(ctx, estimateWait)
	defer cancel()
	if d.c.Hooks == nil {
		return
	}
	for _, line := range d.c.Hooks.BeforeEnvironment(ctx, d.run, d.plan, environment, source) {
		if ctx.Err() == nil {
			d.emit(ctx, model.EventProgress, DescribeEnvironment(environment)+": "+line)
		}
	}
}

// remaining are what is left to build in an environment, in its own
// order, each target with what it needs built first there.
func (d *driver) remaining(environment model.Environment, results map[model.TargetID]model.TargetResult) []buildenv.Target {
	planned, _ := d.plan.In(environment)
	var remaining []buildenv.Target
	for _, id := range planned.Order {
		if _, unmet := d.plan.UnmetIn(environment, id); unmet {
			continue
		}
		if result, ok := results[id]; !ok || !result.Outcome.Complete() {
			target, _ := d.plan.Target(id)
			next := buildenv.Target{PlanTarget: target, DependsOn: planned.Dependencies[id]}
			if source, ok := planned.Git[id]; ok {
				next.Git = &source
			}
			if tag, ok := planned.Tags[id]; ok {
				next.Tag = &tag
			}
			remaining = append(remaining, next)
		}
	}
	return remaining
}

// matrixCell is one environment of a matrix run, as the runner keeps it.
type matrixCell struct {
	environment model.Environment
	attempt     int
	remaining   []buildenv.Target
	results     map[model.TargetID]model.TargetResult
	build       *build
}

// matrix builds a MatrixProvider's environments of the check in one run of
// its own, each recorded through an execution of its own, as GitHub's
// workflow builds every release of its matrix at once (the M1's
// suggestion, 2026-10-29, after rc8's GitHub rows: one release's job never
// ran, and the matrix couldn't be recorded as passing the others). An
// environment whose build never ran is left unrecorded with why; one with
// nothing left, or out of attempts, sits the next run out. Its provider
// records what no earlier build reads, so nothing is reused, and installs
// no archives.
func (d *driver) matrix(ctx context.Context, provider buildenv.Provider, environments []model.Environment, revision model.Revision, commit string) error {
	c := d.c
	kept := ctx
	ctx = progress.Keep(progress.Within(ctx, provider.Name()), func(update progress.Update) {
		if update.Level == progress.Info {
			d.emit(kept, model.EventProgress, update.Message)
		}
	})
	for _, environment := range environments {
		d.estimates.Go(func() { d.sayEstimate(kept, environment, revision.Source) })
	}
	for {
		var check store.Check
		if err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
			var err error
			check, err = r.Check(d.run.ID)
			return err
		}); err != nil {
			return err
		}
		var cells []*matrixCell
		for _, environment := range environments {
			executions := slices.DeleteFunc(slices.Clone(check.Executions), func(x model.GuestExecution) bool { return x.Environment != environment })
			slices.SortFunc(executions, func(a, b model.GuestExecution) int { return a.Attempt - b.Attempt })
			cell := &matrixCell{environment: environment, results: evidence.Merged(executions, check.Results)}
			if len(executions) > 0 {
				last := executions[len(executions)-1]
				cell.attempt = last.Attempt
				switch last.State {
				case model.ExecutionFinished, model.ExecutionCanceled:
					continue
				case model.ExecutionWaiting, model.ExecutionRunning:
					if err := d.endExecution(ctx, last, model.ExecutionInfrastructure, "the process running it ended"); err != nil {
						return err
					}
				}
			}
			if cell.remaining = d.remaining(environment, cell.results); len(cell.remaining) == 0 {
				continue
			}
			if cell.attempt >= model.MaxAttempts {
				d.problem(fmt.Sprintf("%s failed %d times because of an environment error. See dockhand logs %s", DescribeEnvironment(environment), cell.attempt, d.run.Name()))
				continue
			}
			cells = append(cells, cell)
		}
		if len(cells) == 0 {
			return nil
		}
		var job buildenv.Job
		attempt := 0
		for _, cell := range cells {
			execution := model.GuestExecution{ID: model.ExecutionID(store.NewID(cell.environment.Provider)), Run: d.run.ID, Environment: cell.environment, Attempt: cell.attempt + 1, State: model.ExecutionWaiting, CreatedAt: c.Now()}
			if err := d.fenced(ctx, func(tx store.Tx) error {
				if err := tx.AddExecution(execution); err != nil {
					return err
				}
				execution.State = model.ExecutionRunning
				if err := tx.UpdateExecution(execution); err != nil {
					return err
				}
				_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: model.EventExecutionState, Level: model.LevelInfo,
					Message: fmt.Sprintf("%s: attempt %d of %d on %s, run %s", d.run.Name(), execution.Attempt, model.MaxAttempts, DescribeEnvironment(cell.environment), execution.ID)})
				return err
			}); err != nil {
				return err
			}
			cellContext := progress.Within(ctx, DescribeEnvironment(cell.environment))
			cell.build = &build{d: d, ctx: cellContext, execution: execution, tree: revision.Source.Tree, results: cell.results, inputs: map[model.TargetID]model.TargetInputs{}, fetched: map[model.TargetID]model.ObjectID{}}
			job.Matrix = append(job.Matrix, buildenv.MatrixCell{Environment: cell.environment, Execution: execution, Targets: cell.remaining, Build: cell.build})
			attempt = max(attempt, execution.Attempt)
		}
		failpoint.Hit("check.running")
		first := job.Matrix[0]
		job.Run, job.Execution, job.Revision, job.Plan, job.Environment, job.Targets, job.Commit = d.run, first.Execution, revision, d.plan, first.Environment, first.Targets, commit
		job.Directory = filepath.Join(c.Logs, d.run.Name(), fmt.Sprintf("%s-%d", provider.Name(), attempt))
		run := &matrixBuild{build: cells[0].build, name: provider.Name(), cells: cells}
		stopWatching := watchStalls(ctx, cells[0].build, job.Directory)
		err := provider.Execute(ctx, job, run)
		stopWatching()
		_ = os.Remove(job.Directory)
		var unrun *buildenv.Unrun
		retry := false
		for _, cell := range cells {
			cell.build.blockRemaining(cell.remaining)
			execution := cell.build.execution
			var endErr error
			switch {
			case ctx.Err() != nil && d.stopped():
				endErr = d.endExecution(ctx, execution, model.ExecutionInfrastructure, "interrupted: the process driving it stopped")
			case ctx.Err() != nil:
				endErr = d.endExecution(ctx, execution, model.ExecutionCanceled, "canceled")
			case errors.As(err, &unrun):
				// A build that never ran is left unrecorded, with why, and
				// no attempt is spent on it: submit names it, and what
				// checks the release instead.
				words := unrun.Cells[buildenv.EnvironmentKey(cell.environment)]
				if words != "" {
					d.emit(ctx, model.EventProgress, DescribeEnvironment(cell.environment)+": "+words)
				}
				endErr = d.endExecution(ctx, execution, model.ExecutionFinished, words)
			case errors.Is(err, buildenv.ErrNeedsAttention):
				endErr = d.endExecution(ctx, execution, model.ExecutionInfrastructure, err.Error())
			case errors.Is(err, buildenv.ErrDockhandFault):
				endErr = d.endExecution(ctx, execution, model.ExecutionInfrastructure, err.Error())
			case refusal.Transient(err):
				endErr, retry = d.endExecution(ctx, execution, model.ExecutionInfrastructure, err.Error()), true
			case err != nil:
				endErr = d.endExecution(ctx, execution, model.ExecutionInfrastructure, err.Error())
			default:
				endErr = d.endExecution(ctx, execution, model.ExecutionFinished, "")
			}
			if endErr != nil {
				return endErr
			}
		}
		where := provider.Name()
		switch {
		case ctx.Err() != nil, unrun != nil:
			return nil
		case errors.Is(err, buildenv.ErrNeedsAttention):
			d.problem(fmt.Sprintf("%s: %v; another attempt won't fix it, so none was made", where, err))
			return nil
		case errors.Is(err, buildenv.ErrDockhandFault):
			d.problem(fmt.Sprintf("dockhand: %s: %s; the fault is dockhand's own, so another attempt would repeat it, and none was made", where, refusal.WordsOf(err)))
			return nil
		case retry:
			d.emit(ctx, model.EventExecutionRetry, fmt.Sprintf("%s: %v", where, err))
			continue
		case err != nil:
			d.problem(fmt.Sprintf("dockhand: %s: %v; the provider didn't mark it as trouble another attempt could clear, so none was made", where, err))
			return nil
		}
		return nil
	}
}

// matrixBuild is the Build a matrix run's provider reports the run through:
// its progress is the provider's, not one release's, and the run it refers
// to is every cell's. Each cell's results go through its own Build.
type matrixBuild struct {
	*build
	name  string
	cells []*matrixCell
}

func (m *matrixBuild) Progress(message string) {
	m.d.emit(m.ctx, model.EventProgress, m.name+": "+message)
}

func (m *matrixBuild) Waiting(message string) {
	m.d.emit(m.ctx, model.EventWaiting, m.name+": "+message)
}

func (m *matrixBuild) Refer(ref string) error {
	for _, cell := range m.cells {
		if err := cell.build.Refer(ref); err != nil {
			return err
		}
	}
	return nil
}

// unreadIdentity records an attempt that ended before it began, since what
// the environment is couldn't be read.
func (d *driver) unreadIdentity(ctx context.Context, environment model.Environment, attempt int, cause error) error {
	execution := model.GuestExecution{ID: model.ExecutionID(store.NewID(environment.Provider)), Run: d.run.ID, Environment: environment, Attempt: attempt + 1, State: model.ExecutionWaiting, CreatedAt: d.c.Now()}
	if err := d.fenced(ctx, func(tx store.Tx) error { return tx.AddExecution(execution) }); err != nil {
		return err
	}
	detail := "could not identify the build environment: " + cause.Error()
	d.emit(ctx, model.EventExecutionRetry, fmt.Sprintf("%s: %s", DescribeEnvironment(environment), detail))
	return d.endExecution(ctx, execution, model.ExecutionInfrastructure, detail)
}

func (d *driver) endExecution(ctx context.Context, execution model.GuestExecution, state model.ExecutionState, detail string) error {
	now := d.c.Now()
	execution.State, execution.FinishedAt, execution.Detail = state, &now, detail
	return d.fenced(ctx, func(tx store.Tx) error { return tx.UpdateExecution(execution) })
}

// finish settles the run's state from its results.
func (d *driver) finish(ctx context.Context) (model.Run, error) {
	var check evidence.Evidence
	err := d.c.Store.View(ctx, d.c.Repository, func(r store.Reader) error {
		record, err := LoadCheck(r, d.run, false)
		check = evidence.Of(record)
		return err
	})
	if err != nil {
		return d.run, err
	}
	var failed, incomplete []string
	for _, target := range check.Targets {
		for i, result := range target.Outcomes {
			if result.Kind == evidence.CellExcluded {
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
				problem := fmt.Sprintf("%s isn't built on %s: it %s", name, DescribeEnvironment(d.plan.Environments[i]), UnmetWords(result.Unmet))
				if remedy := d.c.Remedy(result.Unmet); remedy != "" {
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
		now := d.c.Now()
		d.run.State, d.run.FinishedAt, d.run.Detail = state, &now, detail
		if err := tx.UpdateRun(d.run); err != nil {
			return err
		}
		_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: model.EventRunState, Level: model.LevelInfo, Message: fmt.Sprintf("%s %s: %s", d.run.Name(), state, detail)})
		return err
	})
	// A check that failed queues the baseline it points to, where
	// check.baseline asks, whichever path ran it (Then's item 4).
	if err == nil && state == model.RunFailed && d.c.Hooks != nil {
		d.c.Hooks.AfterFinish(ctx, d.run)
	}
	if err == nil {
		// Its logs are kept compressed from now on (D6); one that can't be
		// is said, and cleanup tries again.
		if _, compressErr := buildlog.CompressAll(filepath.Join(d.c.Logs, d.run.Name())); compressErr != nil {
			d.emit(ctx, model.EventProgress, fmt.Sprintf("Could not compress %s's logs; cleanup will retry: %v", d.run.Name(), compressErr))
		}
	}
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
	// them (TargetReport.Active), until its result is recorded.
	inputs map[model.TargetID]model.TargetInputs
	// fetched are the commits Git-fetched targets' builds checked out, as
	// the provider reported them (TargetReport.Fetched), until each result is recorded.
	fetched map[model.TargetID]model.ObjectID
	// sourced are the PortGroup files each target's build loaded, as the
	// provider reported them (TargetReport.Sourced), for its inputs.
	sourced map[model.TargetID]map[string]model.ObjectID
	// installs are the kept archives the guest installs targets from.
	installs []buildenv.Archive
	// builtWith are the PortGroups those archives' builds loaded, where
	// known, by their digests.
	builtWith builtWith
	// active are the ports the provider reported active as targets
	// built (TargetReport.Active), whose archives KeepDependency may keep.
	active []model.ActivePort
}

func (b *build) Canceled() bool { return b.ctx.Err() != nil && !b.d.stopped() }

func (b *build) Blocked(target model.TargetID) (model.TargetID, bool) {
	if _, ok := b.d.plan.Target(target); !ok {
		return "", false
	}
	// A dependency interrupted in an earlier attempt is built again, and
	// blocks nothing.
	for _, dep := range b.d.plan.DependsOnIn(b.execution.Environment, target) {
		if result, ok := b.results[dep]; ok && result.Outcome.Complete() && result.Outcome != model.OutcomePassed {
			return dep, true
		}
	}
	return "", false
}

// Record records a target's report: what its provider saw of the build,
// then its result.
func (b *build) Record(report buildenv.TargetReport) error {
	target := report.Result.Target
	if report.Fetched != "" {
		b.fetchedBy(target, report.Fetched)
	}
	if report.Sourced != nil {
		if b.sourced == nil {
			b.sourced = map[model.TargetID]map[string]model.ObjectID{}
		}
		b.sourced[target] = report.Sourced
	}
	if report.Active != nil {
		b.activeIn(target, report.Active)
	}
	result := report.Result
	result.Links, result.Installed, result.Libraries, result.Configure = report.Links, report.Installed, report.Libraries, report.Configure
	return b.record(result)
}

// activeIn completes what the provider saw active as a target built with
// the revision's trees (reuse.Inputs). Inputs that can't be read are left
// unknown, and a result stands without them.
func (b *build) activeIn(target model.TargetID, active []model.ActivePort) {
	planned, ok := b.d.plan.Target(target)
	if !ok {
		return
	}
	// A port the guest was to install from its kept archive, active from
	// another, is one MacPorts got otherwise: said, since the build read
	// that one.
	for _, port := range active {
		for _, archive := range b.installs {
			// A dependency's kept archives are offered, not required:
			// MacPorts takes the one it wants, or none.
			if archive.Target == "" {
				continue
			}
			if strings.EqualFold(port.Name, archive.Port) && port.Archive != "" && port.Archive != archive.Digest {
				b.Progress(fmt.Sprintf("%s built with %s from another archive than the one kept of its build: MacPorts chose %s", target, port.Name, port.Archive))
			}
		}
	}
	// What it installed from a dependency's kept archive is said, by the
	// digest it was active from, once for the target it was first
	// installed for.
	var took []string
	for _, port := range active {
		for _, archive := range b.installs {
			if archive.Target == "" && port.Archive != "" && port.Archive == archive.Digest && strings.EqualFold(port.Name, archive.Port) && !b.consumed(port) && !slices.Contains(took, port.Name) {
				took = append(took, port.Name)
			}
		}
	}
	if len(took) > 0 {
		b.Progress(fmt.Sprintf("%s: installed %s from archives earlier checks kept: %s", target, dependencyCount(len(took)), strings.Join(took, ", ")))
	}
	b.active = append(b.active, active...)
	if inputs, ok := b.read(planned, active); ok {
		b.inputs[target] = inputs
	}
}

// read is what a target's build read of the revision, with the ports its
// provider saw active, nil where it couldn't say, and the PortGroups it
// loaded, where it said (reuse.Inputs). Inputs that can't be read are
// left unknown, and said to be. A port active from a kept archive the
// check offered, which the guest built nothing of and so said no
// PortGroups for, takes those that archive's build loaded, where they are
// known (builtWith): the target read what that build made, as it would
// have had the guest built the port itself, so admission and reuse read
// one provenance (Codex's review of 03b380f1, F1).
func (b *build) read(planned model.PlanTarget, active []model.ActivePort) (model.TargetInputs, bool) {
	inputs, err := reuse.Inputs(b.ctx, b.d.c.Checkout, b.tree, b.execution.Identity, planned, b.withBuiltWith(active), b.sourced[planned.ID])
	if err != nil {
		reason := fmt.Sprintf("what its build read wasn't recorded: %v", err)
		b.Progress(fmt.Sprintf("%s: %s", planned.ID, reason))
		// Where reuse could have been, the reason is kept, for the
		// check that builds it again to say (the architecture review's
		// L3a); a provider that can't say what the environment is
		// reuses nothing anyway.
		if b.execution.Identity == "" {
			return model.TargetInputs{}, false
		}
		return model.TargetInputs{NotReusable: reason}, true
	}
	return inputs, true
}

// withBuiltWith is the active ports with the PortGroups of the kept
// archives the check offered, for those reported active from one with
// none; the rest, MacPorts' archives and kept ones whose builds' are
// unknown among them, as reported.
func (b *build) withBuiltWith(active []model.ActivePort) []model.ActivePort {
	if len(b.builtWith) == 0 {
		return active
	}
	var enriched []model.ActivePort
	for i, port := range active {
		groups, ok := b.builtWith[port.Archive]
		if !ok || port.PortGroups != nil || !slices.ContainsFunc(b.installs, func(a buildenv.Archive) bool {
			return a.Digest == port.Archive && strings.EqualFold(a.Port, port.Name)
		}) {
			continue
		}
		if enriched == nil {
			enriched = slices.Clone(active)
		}
		enriched[i].PortGroups = maps.Clone(groups)
	}
	if enriched == nil {
		return active
	}
	return enriched
}

// fetchedBy keeps the commit a Git-fetched target's build checked out, for
// its result's inputs and for judging it by its plan (judgeSource). One
// that isn't a commit is no answer.
func (b *build) fetchedBy(target model.TargetID, commit string) {
	if !git.ValidObjectID(commit) {
		b.Progress(fmt.Sprintf("%s: its provider said its build fetched %q, which isn't a commit", target, commit))
		return
	}
	b.fetched[target] = model.ObjectID(commit)
}

// record records a target's result, with what its report said of its
// build.
func (b *build) record(result model.TargetResult) error {
	result = b.d.plan.Tests.Judge(result)
	if result.Tests == model.TestsFailed {
		result.Detail = withFailedTests(result.Detail, result)
	}
	if result.Outcome == model.OutcomeFailed {
		result.Detail = withCause(result.Detail, result)
	}
	result = b.judgeSource(result)
	result.Execution = b.execution.ID
	if result.RecordedAt.IsZero() {
		result.RecordedAt = b.d.c.Now()
	}
	inputs, read := b.inputs[result.Target]
	// What a Git-fetched target's build fetched is an input of it, which
	// a port fetched otherwise has none of, whatever its provider said. A
	// provider that says what the build fetched but not which ports were
	// active, as a person's command can, leaves those unknown, and the
	// inputs kept incomplete.
	fetched, said := b.fetched[result.Target]
	if _, ok := b.d.plan.GitIn(b.execution.Environment, result.Target); ok && said {
		if planned, found := b.d.plan.Target(result.Target); !read && found {
			inputs, read = b.read(planned, nil)
		}
		inputs.Fetched = fetched
	}
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
		message := fmt.Sprintf("%s: %s %s", DescribeEnvironment(b.execution.Environment), result.Target, result.Outcome)
		switch {
		case result.Phase == model.PhaseDependencies:
			// The dependency's failure is said with MacPorts' words for it.
			message = fmt.Sprintf("%s: %s: %s: %s", DescribeEnvironment(b.execution.Environment), result.Target, result.Phase.FailureWords(), result.Detail)
		case result.Phase != "":
			message += " at " + string(result.Phase)
		}
		_, err := b.d.session.Emit(tx, model.Event{Branch: b.d.run.Branch, Run: b.d.run.ID, Target: result.Target, Kind: model.EventTargetResult, Level: model.LevelInfo, Message: message})
		return err
	})
	if err == nil {
		b.results[result.Target] = result
		delete(b.inputs, result.Target)
		delete(b.fetched, result.Target)
	}
	return err
}

// judgeSource judges a Git-fetched target's result by the source its plan
// expected (batch 20). A build that fetched another commit than the one
// git.branch named when the check was planned built another source than
// the check covers, whatever its provider said of it: it fails at fetch.
// That is a verdict, not trouble with the environment. Another attempt
// would expect the same commit and fetch the same other one, since that is
// what the ref names now, and nothing about the environment is wrong; the
// port's source moved under the check, as it does under a checksum when an
// archive is replaced, and a new check, which resolves the ref again,
// builds what it names now. Tart's guest applies the same rule as it
// builds, so nothing is built from the other source; the verdict recorded
// is this one, so the two can't disagree. A build whose provider didn't
// say what it fetched stands as it is, for its own check alone, and that
// is said.
func (b *build) judgeSource(result model.TargetResult) model.TargetResult {
	source, ok := b.d.plan.GitIn(b.execution.Environment, result.Target)
	if !ok || result.Outcome != model.OutcomePassed && result.Outcome != model.OutcomeFailed {
		return result
	}
	fetched, said := b.fetched[result.Target]
	switch {
	case source.Moved(fetched):
		result.Outcome, result.Phase, result.Detail = model.OutcomeFailed, model.PhaseFetch, movedWords(source, fetched)
	case !said:
		b.Progress(fmt.Sprintf("%s: its build didn't say which commit of %s it fetched, so no later check can reuse its result", result.Target, RefWords(source.Ref)))
	}
	return result
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
	b.d.emit(b.ctx, model.EventProgress, DescribeEnvironment(b.execution.Environment)+": "+message)
}

// Detail is a step a follower says only at --verbose (buildenv.Detailer).
func (b *build) Detail(message string) {
	b.d.emitAt(b.ctx, model.EventProgress, model.LevelVerbose, DescribeEnvironment(b.execution.Environment)+": "+message)
}

// Holding is the check whose execution runs in the VM a provider named
// ref, among the running checks: a wait for the Mac's VMs names them.
func (b *build) Holding(ref string) string {
	holding := ""
	_ = b.d.c.Store.View(b.ctx, b.d.c.Repository, func(r store.Reader) error {
		executions, err := r.ExecutionsReferred(ref)
		if err != nil {
			return err
		}
		for _, execution := range executions {
			if run, err := r.Run(execution.Run); err == nil && run.State == model.RunRunning {
				holding = run.Name()
				return nil
			}
		}
		return nil
	})
	return holding
}

func (b *build) Waiting(message string) {
	b.d.emit(b.ctx, model.EventWaiting, DescribeEnvironment(b.execution.Environment)+": "+message)
}

// withCause adds to a failure's detail what its log most likely says made
// it fail, marked as read from the log, beside what the provider said
// (D10): whichever provider built it, the log is on this Mac by now. It's
// read from where the step that failed began, the last the provider
// recorded, where it recorded them: the dependencies' installs before a
// port's own build fill most of its log.
func withCause(detail string, result model.TargetResult) string {
	if result.Log == "" {
		return detail
	}
	file, err := buildlog.Open(result.Log)
	if err != nil {
		return detail
	}
	defer file.Close()
	from := 1
	if len(result.Steps) > 0 {
		from = result.Steps[len(result.Steps)-1].Line
	}
	cause, ok := buildlog.FirstFrom(file, from)
	switch {
	case !ok:
		return detail
	case detail == "":
		return FromItsLog + cause.Line
	}
	return detail + " · " + FromItsLog + cause.Line
}

// FromItsLog marks what withCause read from a failure's log.
const FromItsLog = "from its log: "

// withFailedTests adds to a result whose tests failed the tests its log
// names as failed (buildlog.FailedTests), read from where its test step
// began, under declared tests too, where the target passed and nothing
// read its log: exiv2's said "make: *** [test] Error 8" alone (the exiv2
// field notes, 2026-10-19).
func withFailedTests(detail string, result model.TargetResult) string {
	if result.Log == "" {
		return detail
	}
	file, err := buildlog.Open(result.Log)
	if err != nil {
		return detail
	}
	defer file.Close()
	from := 1
	for _, step := range result.Steps {
		if step.Name == model.StepTest {
			from = step.Line
		}
	}
	failed := buildlog.FailedTests(file, from)
	if len(failed) == 0 {
		return detail
	}
	words := "failed tests, " + FromItsLog + strings.Join(failed, ", ")
	if detail == "" {
		return words
	}
	return detail + " · " + words
}

// blockRemaining records as blocked the targets a provider left without a
// verdict whose changed dependency did not pass.
func (b *build) blockRemaining(targets []buildenv.Target) {
	for _, target := range targets {
		if result, ok := b.results[target.ID]; ok && result.Outcome.Complete() {
			continue
		}
		if _, blocked := b.Blocked(target.ID); blocked {
			_ = b.record(model.TargetResult{Target: target.ID, Outcome: model.OutcomeBlocked})
		}
	}
}

// revisionCommit is a commit holding the revision's files: the commit
// itself, or for a snapshot one made of its tree on the base, kept under
// refs/dockhand/revisions so it is not collected while checks use it.
func (c *Service) revisionCommit(ctx context.Context, revision model.Revision) (string, error) {
	if revision.Kind == model.RevisionCommit {
		return string(revision.Source.Commit), nil
	}
	ref := "refs/dockhand/revisions/" + string(revision.ID)
	if existing, err := c.Checkout.ReadRef(ctx, ref); err != nil {
		return "", err
	} else if existing.Exists {
		return existing.Object, nil
	}
	who := git.Signature{Name: "dockhand", Email: "dockhand@localhost", When: revision.CreatedAt}
	commit, err := c.Checkout.WriteCommit(ctx, git.Commit{Tree: string(revision.Source.Tree), Parents: []string{string(revision.Source.Base)},
		Message: fmt.Sprintf("dockhand %s\n", Describe(revision)), Author: who, Committer: who})
	if err != nil {
		return "", err
	}
	if err := c.Checkout.UpdateRefs(ctx, []git.RefChange{{Name: ref, Desired: git.RefValue{Exists: true, Object: commit}}}); err != nil {
		var conflict *git.RefConflict
		if !errors.As(err, &conflict) {
			return "", err
		}
	}
	return commit, nil
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

// runResources are the runs' lease resources.
func RunResources(runs []model.Run) []string {
	var resources []string
	for _, run := range runs {
		resources = append(resources, RunResource(run.ID))
	}
	return resources
}

// QueuedCheck is a check queued or running, with its branch, what it
// checks, and its plan, and whether it stopped: running, and held by no
// live process (Stopped).
type QueuedCheck struct {
	Run      model.Run
	Branch   model.Branch
	Revision model.Revision
	Plan     model.Plan
	Stopped  bool
	// Progress is a running check's newest progress line, as "building
	// jq (2 of 5): fetch", for live progress; empty for one queued.
	Progress string
	// VMs are the Tart VMs a running check's executions run in, by name,
	// and TartHome the home that reaches them: a person's tart list
	// doesn't see dockhand's (the bazel session's notes, 2026-10-24).
	VMs      []string
	TartHome string
}

// Queue is every check queued or running, oldest first, each with what
// queue and status say of it, read in one transaction with their holders,
// where each took four Views and a Holder's (SQL-O3, O4).
func (c *Service) Queue(ctx context.Context, session *coord.Session) ([]QueuedCheck, error) {
	var queue []QueuedCheck
	err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
		runs, err := r.Runs(store.RunFilter{States: []model.RunState{model.RunQueued, model.RunRunning}})
		if err != nil {
			return err
		}
		slices.Reverse(runs)
		holders, err := session.Holders(r, RunResources(runs)...)
		if err != nil {
			return err
		}
		for _, run := range runs {
			checked := QueuedCheck{Run: run, Stopped: run.State == model.RunRunning && holders[RunResource(run.ID)] == nil}
			if checked.Branch, err = r.Branch(run.Branch); err != nil {
				return err
			}
			if checked.Revision, err = r.Revision(run.Revision); err != nil {
				return err
			}
			if checked.Plan, err = r.Plan(run.Plan); err != nil {
				return err
			}
			if run.State == model.RunRunning && !checked.Stopped {
				event, found, err := r.LatestRunEvent(run.ID, model.EventProgress, model.EventWaiting)
				if err != nil {
					return err
				}
				if found {
					checked.Progress = event.Message
				}
				executions, err := r.Executions(run.ID)
				if err != nil {
					return err
				}
				for _, execution := range executions {
					if execution.Environment.Provider == buildenv.Tart && execution.State == model.ExecutionRunning && execution.ProviderRef != "" {
						checked.VMs = append(checked.VMs, execution.ProviderRef)
					}
				}
				checked.TartHome = c.TartHome
			}
			queue = append(queue, checked)
		}
		return nil
	})
	return queue, err
}

// RunProviders are the providers a run's plan builds on, each once.
func (c *Service) RunProviders(ctx context.Context, run model.Run) ([]string, error) {
	var providers []string
	err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
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

// dependencyCount is n dependencies, as "1 dependency" or "12 dependencies".
func dependencyCount(n int) string {
	if n == 1 {
		return "1 dependency"
	}
	return fmt.Sprintf("%d dependencies", n)
}

// offeredWords say how many dependencies a guest is offered kept archives
// of, and how many archives where a port has two.
func offeredWords(ports, archives int) string {
	words := "the guest may install up to " + dependencyCount(ports) + " from archives earlier checks kept"
	if archives != ports {
		words += fmt.Sprintf(" (%d archives, the newest two of some)", archives)
	}
	return words
}

// stallAfter is how long a target's log may go without growing before it's
// said, and stallPoll how often it's looked at; a service's Stall sets
// others, as a test's.
const stallAfter, stallPoll = 10 * time.Minute, time.Minute

// watchStalls says, once, at every level, a target whose log in an
// execution's directory hasn't grown for stallAfter: arowpp's bazel client
// exited and its server held port's pipe open, and the VM sat idle for 36
// minutes with nothing said (the bazel session's notes, 2026-10-24). The
// newest log is the target building. It says it again only once the log
// has grown since. The returned function stops it.
func watchStalls(ctx context.Context, build *build, directory string) func() {
	after, poll := stallAfter, stallPoll
	if s := build.d.c.Stall; s[0] > 0 {
		after, poll = s[0], s[1]
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var watched string
		var size int64
		var since time.Time
		said := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(poll):
			}
			logs, _ := filepath.Glob(filepath.Join(directory, "*.log"))
			newest, newestAt, newestSize := "", time.Time{}, int64(0)
			for _, path := range logs {
				if info, err := os.Stat(path); err == nil && info.ModTime().After(newestAt) {
					newest, newestAt, newestSize = path, info.ModTime(), info.Size()
				}
			}
			now := time.Now()
			switch {
			case newest == "":
				continue
			case newest != watched || newestSize != size:
				watched, size, since, said = newest, newestSize, now, false
			case !said && now.Sub(since) >= after:
				said = true
				build.Waiting(fmt.Sprintf("%s's log hasn't grown in %s", strings.TrimSuffix(filepath.Base(newest), ".log"), prose.Age(after)))
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
