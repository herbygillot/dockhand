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

// earlier are the targets an environment has left to build, each with
// its newest passed builds there that recorded what they read (Reusable),
// and the paths those builds read.
func (d *driver) earlier(ctx context.Context, environment model.Environment, remaining []buildenv.Target) ([]reuse.Target, []string, error) {
	targets := make([]reuse.Target, len(remaining))
	var paths []string
	err := d.e.Store.View(ctx, d.e.Repository, func(r store.Reader) error {
		for i, target := range remaining {
			targets[i] = reuse.Target{PlanTarget: target.PlanTarget, DependsOn: target.DependsOn, Git: target.Git}
			builds, err := r.Reusable(target.ID, environment, reuseCandidates)
			if err != nil {
				return err
			}
			for _, build := range builds {
				inputs, err := r.Inputs(build.Result.Inputs)
				if err != nil {
					return err
				}
				targets[i].Earlier = append(targets[i].Earlier, reuse.Candidate{Result: build.Result, Inputs: inputs, Origin: build.Execution})
				paths = append(paths, reuse.Paths(inputs)...)
			}
		}
		return nil
	})
	return targets, paths, err
}

// reusable chooses the targets that reuse an earlier build instead of
// building, before an environment's first attempt (reuse.Choose, decision
// 28): each target's newest earlier build that stands and read what it
// would read now, in the environment of its identity now. A reused target
// one that builds needs is installed from its kept archive, or builds. A
// check with Fresh reuses nothing, and nor does an environment that can't
// say what it is.
func (d *driver) reusable(ctx context.Context, identity string, targets []reuse.Target, paths []string, tree model.ObjectID) (reuse.Choice, error) {
	if d.plan.Fresh || identity == "" || len(paths) == 0 {
		return reuse.Choice{}, nil
	}
	slices.Sort(paths)
	objects, err := d.e.Repo.Directories(ctx, string(tree), slices.Compact(paths))
	if err != nil {
		return reuse.Choice{}, err
	}
	trees := map[string]model.ObjectID{}
	for path, object := range objects {
		trees[path] = model.ObjectID(object)
	}
	available := func(c reuse.Candidate) bool {
		_, kept, err := d.e.keptArchive(ctx, c.Result.Archive)
		return err == nil && kept
	}
	return reuse.Choose(targets, identity, trees, d.plan.Tests.Stands, available), nil
}

// installs are the kept archives the guest installs targets it doesn't
// build from, for the targets it builds that need them (reuse.Needs):
// each a target with a passed result here, reused or finished in an
// earlier attempt. One whose archive isn't kept is left to MacPorts.
func (d *driver) installs(ctx context.Context, environment model.Environment, building []reuse.Target, results map[model.TargetID]model.TargetResult) ([]buildenv.Archive, error) {
	planned, _ := d.plan.In(environment)
	var installs []buildenv.Archive
	for _, target := range building {
		for _, need := range reuse.Needs(target, planned.Order) {
			if slices.ContainsFunc(building, func(t reuse.Target) bool { return t.ID == need }) || slices.ContainsFunc(installs, func(a buildenv.Archive) bool { return a.Target == need }) {
				continue
			}
			result, ok := results[need]
			if !ok || result.Outcome != model.OutcomePassed || result.Archive == "" {
				continue
			}
			archive, kept, err := d.e.keptArchive(ctx, result.Archive)
			if err != nil {
				return nil, err
			}
			if !kept {
				continue
			}
			planned, _ := d.plan.Target(need)
			port := planned.Target.Name
			if planned.Target.Subport != "" {
				port = planned.Target.Subport
			}
			installs = append(installs, buildenv.Archive{Target: need, Port: port, Name: archive.Name, Digest: archive.Digest, Path: d.e.archivePath(archive.Digest)})
		}
	}
	return installs, nil
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
