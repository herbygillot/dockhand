package engine

import (
	"context"
	"fmt"
	"os"
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

// sayNotReusable says, of each target whose newest earlier build here
// stands but couldn't be reused, why it builds again: what that build
// read wasn't recorded (the architecture review's L3a).
func (d *driver) sayNotReusable(ctx context.Context, environment model.Environment, targets []reuse.Target) {
	for _, target := range targets {
		if len(target.Earlier) == 0 {
			continue
		}
		newest := target.Earlier[0]
		if newest.Inputs.NotReusable == "" || !d.plan.Tests.Stands(newest.Result) {
			continue
		}
		d.emit(ctx, "execution.state", fmt.Sprintf("%s: %s: %s builds again: its last build here passed, but can't be reused, since %s", d.run.Name(), describeEnvironment(environment), target.ID, newest.Inputs.NotReusable))
	}
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
			// A kept archive changed since it was kept, by a byte or more,
			// isn't given to the guest: MacPorts builds the target instead,
			// and the check says why (prime-time D-T1). Signing would refuse
			// it too, and fail the check, where a build does the work.
			if _, digest, err := sha256File(d.e.archivePath(archive.Digest)); err != nil || digest != archive.Digest {
				if err := d.altered(ctx, string(need), archive.Digest, "the guest builds it instead"); err != nil {
					return nil, err
				}
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

// dependencyInstalls are the kept archives of the ports the targets an
// environment builds depend on, which earlier guests there installed them
// from (batch 90): a target's direct dependencies, as MacPorts evaluates
// it in the revision, and the ports active as its earlier builds there
// built, which reach further. The guest's MacPorts takes one whose name is
// the archive it wants, and builds the rest as before, so rust and cargo,
// built from source once on a release MacPorts has no archives of them
// for, aren't built again by a later check of any port needing them. A
// port the plan builds is the branch's, and none of its are given. One
// changed since it was kept isn't given.
func (d *driver) dependencyInstalls(ctx context.Context, environment model.Environment, source model.Source, building []reuse.Target) []buildenv.Archive {
	var ports []string
	add := func(port string) {
		if port == "" || slices.Contains(ports, port) {
			return
		}
		for _, target := range d.plan.Targets {
			if strings.EqualFold(target.Target.Name, port) || strings.EqualFold(target.Target.Subport, port) {
				return
			}
		}
		ports = append(ports, port)
	}
	reader, err := d.e.portReader()
	for _, target := range building {
		if err == nil {
			name := target.Target.Name
			if target.Target.Subport != "" {
				name = target.Target.Subport
			}
			evaluated, evalErr := reader.Ports(ctx, source, target.Directory, environment, nil)
			for _, port := range evaluated {
				if evalErr != nil || port.Name != name {
					continue
				}
				for _, dependency := range port.Dependencies {
					add(dependency.Port)
				}
			}
		}
		for _, earlier := range target.Earlier {
			for _, active := range earlier.Inputs.Active {
				add(active.Name)
			}
		}
	}
	var kept []model.DependencyArchive
	if err := d.e.Store.View(ctx, d.e.Repository, func(r store.Reader) error {
		var err error
		kept, err = r.DependencyArchives(environment, ports)
		return err
	}); err != nil {
		return nil
	}
	var installs []buildenv.Archive
	for _, archive := range kept {
		if _, whole, err := d.e.keptArchive(ctx, archive.Digest); err != nil || !whole {
			continue
		}
		if _, digest, err := sha256File(d.e.archivePath(archive.Digest)); err != nil || digest != archive.Digest {
			// Said, as a target's is, where it was skipped in silence (the
			// rc8 full stage's D-T1). What can't be said leaves the guest
			// to fetch the dependency as before.
			_ = d.altered(ctx, archive.Port, archive.Digest, "the guest gets it as MacPorts would, from its archives or a build")
			continue
		}
		installs = append(installs, buildenv.Archive{Port: archive.Port, Name: archive.Name, Digest: archive.Digest, Path: d.e.archivePath(archive.Digest)})
	}
	return installs
}

// AlteredKind is the event a kept archive changed since it was kept is
// said by: check prints it, and its --json result carries it.
const AlteredKind = "archive.altered"

// altered says a kept archive that isn't the one it was kept as, by a
// byte or more, as tampering or a disk's fault leaves one, and sets the
// file aside beside it, as <file>.altered, so no later check meets it
// again, and what it was is there to look at. Its record goes as an
// archive's whose file is gone does, at cleanup. keptArchive, which
// compares only the size, is the cheap test reuse and cleanup make; the
// digest is read here, before a guest is given one (prime-time D-T1).
func (d *driver) altered(ctx context.Context, port, digest, instead string) error {
	path := d.e.archivePath(digest)
	aside := ""
	if os.Rename(path, path+".altered") == nil {
		aside = "; it's set aside as " + path + ".altered"
	}
	return d.fenced(ctx, func(tx store.Tx) error {
		_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: AlteredKind, Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: the archive kept of %s isn't the one it was kept as, %s, so %s%s", d.run.Name(), port, digest, instead, aside)})
		return err
	})
}
