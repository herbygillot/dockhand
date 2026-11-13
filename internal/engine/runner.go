package engine

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// pollInterval is how often a check's driver looks for a cancel request:
// Options.Poll, a second unless a test sets it.
func (e *Engine) pollInterval() time.Duration {
	if e.options.Poll > 0 {
		return e.options.Poll
	}
	return time.Second
}

// FailureLines are the last lines, at most n, of the step a target failed
// at in an environment, read from its log where no reading of it found the
// cause (checks.FromItsLog): what a failure printed last, said in the
// check's own report rather than left to dockhand logs (the Vx port's
// field testing, 2026-10-03). The step ends where another target's step in the same log
// begins. None for a result that didn't fail, or has no log.
func FailureLines(evidence Evidence, target TargetEvidence, environment, n int) []string {
	if environment >= len(target.Outcomes) {
		return nil
	}
	result := target.Outcomes[environment].TargetResult
	if result.Outcome != model.OutcomeFailed || result.Log == "" || strings.Contains(result.Detail, checks.FromItsLog) {
		return nil
	}
	from := 1
	if len(result.Steps) > 0 {
		from = result.Steps[len(result.Steps)-1].Line
	}
	to := 0
	for _, other := range evidence.Targets {
		if other.Target.ID == target.Target.ID || environment >= len(other.Outcomes) || other.Outcomes[environment].Log != result.Log {
			continue
		}
		for _, step := range other.Outcomes[environment].Steps {
			if step.Line > from && (to == 0 || step.Line < to) {
				to = step.Line
			}
		}
	}
	file, err := buildlog.Open(result.Log)
	if err != nil {
		return nil
	}
	defer file.Close()
	return buildlog.Last(file, from, to, n)
}

// RunEvidenceNow is RunEvidence with its environments' identities read
// now, outside any transaction, so it says which couldn't be read, as a
// check's report and wait's must (evidence.OfNow).
func (e *Engine) RunEvidenceNow(ctx context.Context, id model.RunID) (Evidence, error) {
	run, err := e.Run(ctx, id)
	if err != nil {
		return Evidence{}, err
	}
	plan, err := e.Plan(ctx, run.Plan)
	if err != nil {
		return Evidence{}, err
	}
	now := e.identitiesNow(ctx, plan.Environments)
	var found Evidence
	err = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		check, err := checks.LoadCheck(r, run)
		if err != nil {
			return err
		}
		found = evidence.OfNow(check, now)
		return nil
	})
	return found, err
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
		evidence, err = runEvidence(r, run)
		return err
	})
	return evidence, err
}

// runEvidence is what a run established for each target in each of its
// environments, as its own record says (evidence.Of).
func runEvidence(r store.Reader, run model.Run) (Evidence, error) {
	check, err := checks.LoadCheck(r, run)
	if err != nil {
		return Evidence{}, err
	}
	return evidence.Of(check), nil
}

// LogDirectory is where check logs are kept: logs beside the database.
func (e *Engine) LogDirectory() string {
	return filepath.Join(filepath.Dir(e.options.Database), "logs")
}

// LaunchDirectory is where attempts' launch locks are kept, beside the
// database (checks.Service.Launches).
func (e *Engine) LaunchDirectory() string {
	return filepath.Join(filepath.Dir(e.options.Database), "launches")
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

// Events reads the journal past a sequence number, at least as important
// as level (every level where it is empty), oldest first: what
// observers tail (Design v3 §11).
func (e *Engine) Events(ctx context.Context, after int64, level model.EventLevel) ([]model.Event, error) {
	var events []model.Event
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		for {
			batch, err := r.Events(after, 500, level)
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

// CheckSummary is a check with what it checks: its branch, revision,
// and plan, and what its dependencies' kept archives said, gone or altered.
type CheckSummary struct {
	Branch   model.Branch
	Revision model.Revision
	Plan     model.Plan
	Altered  []string
}

// CheckSummary reads a check's summary in one transaction, where its four
// reads were each a View and could see four states (SQL-O3).
func (e *Engine) CheckSummary(ctx context.Context, run model.Run) (CheckSummary, error) {
	var summary CheckSummary
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if summary.Branch, err = r.Branch(run.Branch); err != nil {
			return err
		}
		if summary.Revision, err = r.Revision(run.Revision); err != nil {
			return err
		}
		if summary.Plan, err = r.Plan(run.Plan); err != nil {
			return err
		}
		for after := int64(0); ; {
			events, err := r.RunEvents(run.ID, after, 500)
			if err != nil || len(events) == 0 {
				return err
			}
			for _, event := range events {
				if event.Kind == model.EventArchiveAltered || event.Kind == model.EventArchiveGone {
					summary.Altered = append(summary.Altered, event.Message)
				}
			}
			after = events[len(events)-1].Sequence
		}
	})
	return summary, err
}
