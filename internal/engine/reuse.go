package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/reuse"
	"github.com/herbygillot/dockhand/internal/store"
)

// reuseCandidates is how many of a target's earlier results in an
// environment reuse considers, newest first.
const reuseCandidates = 5

// candidate is an earlier result that may stand for a build now, with what
// its build read and the execution that built it.
type candidate struct {
	result model.TargetResult
	inputs model.TargetInputs
	origin model.GuestExecution
}

// reuse records an execution that builds nothing, when every target the
// environment has left to build has an earlier passed result whose build
// read what its build would read now (reuse.Current, decision 28): the
// provider isn't started at all. A target the provider has to build leaves
// all of them to it, for now; reusing some and building the rest needs the
// reused ones' archives in the guest. A check with Fresh reuses nothing.
func (d *driver) reuse(ctx context.Context, environment model.Environment, remaining []buildenv.Target, tree model.ObjectID) (bool, error) {
	if d.plan.Fresh || len(remaining) == 0 {
		return false, nil
	}
	identity := d.e.identitiesNow(ctx, []model.Environment{environment})[environment]
	if identity == "" {
		return false, nil
	}
	found := make([][]candidate, len(remaining))
	var paths []string
	if err := d.e.Store.View(ctx, d.e.Repository, func(r store.Reader) error {
		for i, target := range remaining {
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
				found[i] = append(found[i], candidate{result: result, inputs: inputs, origin: origin})
				paths = append(paths, reuse.Paths(inputs)...)
			}
			if len(found[i]) == 0 {
				return nil
			}
		}
		return nil
	}); err != nil {
		return false, err
	}
	if slices.ContainsFunc(found, func(c []candidate) bool { return len(c) == 0 }) {
		return false, nil
	}
	slices.Sort(paths)
	objects, err := d.e.Repo.Directories(ctx, string(tree), slices.Compact(paths))
	if err != nil {
		return false, err
	}
	trees := map[string]model.ObjectID{}
	for path, object := range objects {
		trees[path] = model.ObjectID(object)
	}
	chosen := make([]candidate, len(remaining))
	for i, target := range remaining {
		k := slices.IndexFunc(found[i], func(c candidate) bool {
			return d.plan.Tests.Stands(c.result) && reuse.Current(c.inputs, identity, target.PlanTarget, trees)
		})
		if k < 0 {
			return false, nil
		}
		chosen[i] = found[i][k]
	}
	return true, d.recordReuse(ctx, environment, identity, chosen)
}

// recordReuse records the execution that reuses the chosen results: one
// that ran nothing, in the environment as it is now, reporting what the
// newest build it reuses found of the environment, with each target's
// result as it was, naming the execution that built it.
func (d *driver) recordReuse(ctx context.Context, environment model.Environment, identity string, chosen []candidate) error {
	now := d.e.now()
	newest := chosen[0].origin
	var checks []string
	for _, c := range chosen {
		if c.origin.CreatedAt.After(newest.CreatedAt) {
			newest = c.origin
		}
	}
	execution := model.GuestExecution{ID: model.ExecutionID(store.NewID(environment.Provider)), Run: d.run.ID, Environment: environment, Identity: identity, Reused: true,
		Attempt: 1, State: model.ExecutionFinished, Observed: newest.Observed, CreatedAt: now, FinishedAt: &now}
	return d.fenced(ctx, func(tx store.Tx) error {
		if err := tx.AddExecution(execution); err != nil {
			return err
		}
		for _, c := range chosen {
			result := c.result
			result.Execution, result.ReusedFrom, result.RecordedAt = execution.ID, c.result.Execution, now
			if err := tx.RecordResult(result); err != nil {
				return err
			}
			if run, err := tx.Run(c.origin.Run); err == nil && !slices.Contains(checks, run.Name()) {
				checks = append(checks, run.Name())
			}
		}
		_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: "execution.state", Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: %s: every target would build as it did in %s, and reuses that result, building nothing", d.run.Name(), describeEnvironment(environment), strings.Join(checks, ", "))})
		return err
	})
}
