package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/reuse"
	"github.com/herbygillot/dockhand/internal/store"
)

// reuseCandidates is how many of a target's earlier results in an
// environment reuse considers, newest first.
const reuseCandidates = 5

// reusable chooses the targets an environment has left to build that reuse
// an earlier build instead, before its first attempt (reuse.Choose,
// decision 28): each target's newest passed builds there that recorded
// what they read, in the environment of its identity now. A check with
// Fresh reuses nothing, and nor does an environment that can't say what it
// is.
func (d *driver) reusable(ctx context.Context, environment model.Environment, identity string, remaining []buildenv.Target, tree model.ObjectID) (map[model.TargetID]reuse.Candidate, error) {
	if d.plan.Fresh || identity == "" || len(remaining) == 0 {
		return nil, nil
	}
	targets := make([]reuse.Target, len(remaining))
	var paths []string
	if err := d.e.Store.View(ctx, d.e.Repository, func(r store.Reader) error {
		for i, target := range remaining {
			targets[i] = reuse.Target{PlanTarget: target.PlanTarget, DependsOn: target.DependsOn}
			results, err := r.Reusable(target.ID, environment, reuseCandidates)
			if err != nil {
				return err
			}
			for _, result := range results {
				inputs, err := r.Inputs(result.Inputs)
				if err != nil {
					return err
				}
				origin, err := r.Execution(result.Execution)
				if err != nil {
					return err
				}
				targets[i].Earlier = append(targets[i].Earlier, reuse.Candidate{Result: result, Inputs: inputs, Origin: origin})
				paths = append(paths, reuse.Paths(inputs)...)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	slices.Sort(paths)
	objects, err := d.e.Repo.Directories(ctx, string(tree), slices.Compact(paths))
	if err != nil {
		return nil, err
	}
	trees := map[string]model.ObjectID{}
	for path, object := range objects {
		trees[path] = model.ObjectID(object)
	}
	return reuse.Choose(targets, identity, trees, d.plan.Tests.Stands), nil
}

// recordReuse records the execution that reuses every target's earlier
// build: one that ran nothing, in the environment as it is now, reporting
// what the newest build it reuses found of the environment. The provider
// isn't started at all.
func (d *driver) recordReuse(ctx context.Context, environment model.Environment, identity string, remaining []buildenv.Target, chosen map[model.TargetID]reuse.Candidate) error {
	now := d.e.now()
	ordered := reusedInOrder(remaining, chosen)
	newest := ordered[0].Origin
	for _, c := range ordered {
		if c.Origin.CreatedAt.After(newest.CreatedAt) {
			newest = c.Origin
		}
	}
	execution := model.GuestExecution{ID: model.ExecutionID(store.NewID(environment.Provider)), Run: d.run.ID, Environment: environment, Identity: identity, Reused: true,
		Attempt: 1, State: model.ExecutionFinished, Observed: newest.Observed, CreatedAt: now, FinishedAt: &now}
	return d.fenced(ctx, func(tx store.Tx) error {
		if err := tx.AddExecution(execution); err != nil {
			return err
		}
		checks, err := recordReused(tx, execution.ID, ordered, now)
		if err != nil {
			return err
		}
		_, err = d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: "execution.state", Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: %s: every target would build as it did in %s, and reuses that result, building nothing", d.run.Name(), describeEnvironment(environment), strings.Join(checks, ", "))})
		return err
	})
}

// reuses records, in the execution that builds the rest, the results of
// the targets that reuse an earlier build, before the provider starts.
func (b *build) reuses(remaining []buildenv.Target, chosen map[model.TargetID]reuse.Candidate) error {
	now := b.d.e.now()
	ordered := reusedInOrder(remaining, chosen)
	var names []string
	for _, c := range ordered {
		names = append(names, string(c.Result.Target))
	}
	err := b.d.fenced(b.ctx, func(tx store.Tx) error {
		checks, err := recordReused(tx, b.execution.ID, ordered, now)
		if err != nil {
			return err
		}
		_, err = b.d.session.Emit(tx, model.Event{Branch: b.d.run.Branch, Run: b.d.run.ID, Kind: "execution.state", Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: %s: %s would build as in %s, and reuse those results; the rest build", b.d.run.Name(), describeEnvironment(b.execution.Environment), strings.Join(names, ", "), strings.Join(checks, ", "))})
		return err
	})
	if err != nil {
		return err
	}
	for _, c := range ordered {
		b.results[c.Result.Target] = reusedResult(c, b.execution.ID, now)
	}
	return nil
}

// recordReused records each reused result as it was, in the execution that
// reuses it, naming the execution that built it, and says which checks
// those were in.
func recordReused(tx store.Tx, execution model.ExecutionID, ordered []reuse.Candidate, now time.Time) ([]string, error) {
	var checks []string
	for _, c := range ordered {
		if err := tx.RecordResult(reusedResult(c, execution, now)); err != nil {
			return nil, err
		}
		if run, err := tx.Run(c.Origin.Run); err == nil && !slices.Contains(checks, run.Name()) {
			checks = append(checks, run.Name())
		}
	}
	return checks, nil
}

// reusedResult is an earlier result, as the execution that reuses it
// records it.
func reusedResult(c reuse.Candidate, execution model.ExecutionID, now time.Time) model.TargetResult {
	result := c.Result
	result.Execution, result.ReusedFrom, result.RecordedAt = execution, c.Result.Execution, now
	return result
}

// reusedInOrder are the chosen builds, in the environment's order.
func reusedInOrder(remaining []buildenv.Target, chosen map[model.TargetID]reuse.Candidate) []reuse.Candidate {
	var ordered []reuse.Candidate
	for _, target := range remaining {
		if c, ok := chosen[target.ID]; ok {
			ordered = append(ordered, c)
		}
	}
	return ordered
}
