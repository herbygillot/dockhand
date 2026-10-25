package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/refusal"
)

// Beside is what queuing a check does where the branch has one queued or
// running already, since a branch has one check at a time (decision 29).
type Beside int

const (
	// BesideRefuse refuses beside it, with an ActiveRunError that says
	// whether its process stopped: check's, and serve's.
	BesideRefuse Beside = iota
	// BesideFollow follows one for the same files that would stand for this
	// one once it passed (PlanCovers), and refuses beside any other: ship's,
	// so a rerun resumes rather than refuses (decision 4).
	BesideFollow
	// BesideReplace stops what holds the branch, keeping what it finished,
	// and queues this one: check --replace.
	BesideReplace
)

// QueueRequest is a check to queue: a proposal's plan of a revision of the
// branch, for origin.
type QueueRequest struct {
	Branch   model.Branch
	Revision model.Revision
	Plan     model.Plan
	Origin   model.Origin
	// Covered takes a check that passed for these files and stands for
	// this one (Covered) as its result, queuing nothing: ship's, which a
	// rerun doesn't build again.
	Covered bool
	Beside  Beside
	// Stop is asked before a running check is stopped (BesideReplace),
	// where a person is there to say; nil stops it.
	Stop func(model.Run) (bool, error)
}

// Queued is a check queued, or what stood in its place.
type Queued struct {
	// Run is the check queued, or the one already queued or running that's
	// followed (Followed); empty where Passed stands for it.
	Run      model.Run
	Followed bool
	// Passed is the check that passed for these files already, where it
	// stands for this one.
	Passed *model.Run
	// Replaced are the runs stopped, as they are after, beside Was, as they
	// were before.
	Replaced, Was []model.Run
}

// UnrunnableError is a plan nothing can be built from: its unresolved
// targets, or nothing it plans can build where it's asked to.
type UnrunnableError struct {
	Plan model.Plan
}

func (u *UnrunnableError) Error() string {
	if len(u.Plan.Unresolved) > 0 {
		return unresolvedWords(u.Plan)
	}
	return "none of the planned ports can build in the selected environments; see Not built"
}

// Runnable refuses a plan nothing can be built from (UnrunnableError).
func Runnable(plan model.Plan) error {
	if plan.Runnable() {
		return nil
	}
	return &UnrunnableError{Plan: plan}
}

// localAllowed refuses a check in a MacPorts prefix on this Mac where its
// Portfiles would run as root unasked: a pull request someone else wrote,
// which the person adopted (the trust rule, ahead of Then's item 12), and
// serve's checks, unless [providers.local] serve says they may.
func (e *Engine) localAllowed(request QueueRequest) error {
	if !slices.ContainsFunc(request.Plan.Environments, func(environment model.Environment) bool { return environment.Provider == buildenv.Local }) {
		return nil
	}
	if pr := request.Branch.PullRequest; pr != nil && pr.Adopted {
		return refusal.New(refusal.Failed, "%s is a pull request you adopted, whose Portfiles someone else wrote: a local check would run them as root on this Mac, so it checks with --on tart or --on github", request.Branch.ShortName())
	}
	if request.Origin == model.OriginServe && !e.LocalServe {
		return refusal.New(refusal.Environment, "serve doesn't check in a MacPorts prefix on this Mac unless [providers.local] serve = true, since its Portfiles run as root there, under a sudo rule that needs no password")
	}
	return nil
}

// QueueCheck is the one way a check is queued, for check, ship, and serve
// alike (the architecture review of 5897a82, R3-4 and R3-14): a passing
// check that stands for it, where request.Covered takes one; a plan nothing
// can be built from refused; and one already queued or running for the
// branch met as request.Beside says.
func (e *Engine) QueueCheck(ctx context.Context, request QueueRequest) (Queued, error) {
	if request.Covered {
		passed, ok, err := e.Covered(ctx, request.Branch.ID, request.Revision.Source.Tree, request.Plan)
		if err != nil {
			return Queued{}, err
		}
		if ok {
			return Queued{Passed: &passed}, nil
		}
	}
	if err := Runnable(request.Plan); err != nil {
		return Queued{}, err
	}
	if err := e.localAllowed(request); err != nil {
		return Queued{}, err
	}
	queued, err := e.beside(ctx, request)
	if err != nil || queued.Followed {
		return queued, err
	}
	queued.Run, err = e.Enqueue(ctx, request.Branch, request.Plan, request.Origin)
	if err != nil {
		return queued, err
	}
	// The branch's new check replaces the clones its earlier ones kept for
	// dockhand shell; one that can't be removed is clean's to find.
	if err := e.forgetKept(ctx, request.Branch.ID); err != nil {
		progress.Report(ctx, "removing the clones %s's earlier checks kept: %v", request.Branch.ShortName(), err)
	}
	return queued, nil
}

// forgetKept removes the clones the branch's checks kept for dockhand
// shell (model.Plan.Keep).
func (e *Engine) forgetKept(ctx context.Context, branch model.BranchID) error {
	if !slices.ContainsFunc(slices.Collect(maps.Values(e.Providers)), func(provider buildenv.Provider) bool {
		_, ok := provider.(buildenv.LeftoverProvider)
		return ok
	}) {
		return nil
	}
	session, err := e.Session(ctx, model.SessionForeground)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	leftovers, err := e.PlanLeftovers(ctx, session)
	if err != nil {
		return err
	}
	leftovers = slices.DeleteFunc(leftovers, func(leftover Leftover) bool {
		return leftover.KeptAt.IsZero() || leftover.Run == nil || leftover.Run.Branch != branch
	})
	_, err = e.RemoveLeftovers(ctx, session, leftovers)
	return err
}

// beside meets the check queued or running for the branch, where there's
// one, as request.Beside says: refused, as an ActiveRunError judged
// stopped or not; followed, where it's of the same files and its plan
// covers this one; or stopped, keeping what it finished, a queued one
// canceled at once and a running one asked to stop where Stop agrees.
func (e *Engine) beside(ctx context.Context, request QueueRequest) (Queued, error) {
	judging, observer, end, err := e.Observing(ctx)
	if err != nil {
		return Queued{}, err
	}
	active, runs, err := e.ActiveCheck(judging, observer, request.Branch, request.Revision)
	end()
	if err != nil || active == nil {
		return Queued{}, err
	}
	switch request.Beside {
	case BesideRefuse:
		return Queued{}, active
	case BesideFollow:
		if !active.SameRevision {
			return Queued{}, active
		}
		running, err := e.Plan(ctx, active.Run.Plan)
		if err != nil {
			return Queued{}, err
		}
		if !PlanCovers(running, request.Plan) {
			return Queued{}, fmt.Errorf("%s is %s for these files, but does not cover the required environments or test policy. Run dockhand cancel %s, then ship again, or wait for it to finish and ship again", active.Run.Name(), active.Run.State, active.Run.Name())
		}
		return Queued{Run: active.Run, Followed: true}, nil
	}
	for _, run := range runs {
		if run.State != model.RunRunning || request.Stop == nil {
			continue
		}
		ok, err := request.Stop(run)
		if err != nil {
			return Queued{}, err
		}
		if !ok {
			return Queued{}, fmt.Errorf("%s keeps running; nothing new was queued", run.Name())
		}
	}
	session, err := e.Session(ctx, model.SessionForeground)
	if err != nil {
		return Queued{}, err
	}
	defer session.End(context.WithoutCancel(ctx))
	replaced, err := e.Replace(ctx, session, runs)
	return Queued{Replaced: replaced, Was: runs[:len(replaced)]}, err
}
