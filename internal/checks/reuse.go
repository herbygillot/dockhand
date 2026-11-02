package checks

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/reuse"
	"github.com/herbygillot/dockhand/internal/store"
)

// reuseCandidates is how many of a target's earlier results in an
// environment reuse considers, newest first.
const reuseCandidates = 5

// earlier are the targets an environment has left to build, each with
// its newest passed builds there that recorded what they read (Reusable),
// and the paths those builds read. A target fetched by a GitHub tag has
// each build's tag commit from its check's plan (EnvironmentPlan.Tags).
func (d *driver) earlier(ctx context.Context, environment model.Environment, remaining []buildenv.Target) ([]reuse.Target, []string, error) {
	targets := make([]reuse.Target, len(remaining))
	var paths []string
	plans := map[model.RunID]model.Plan{}
	err := d.c.Store.View(ctx, d.c.Repository, func(r store.Reader) error {
		for i, target := range remaining {
			targets[i] = reuse.Target{PlanTarget: target.PlanTarget, DependsOn: target.DependsOn, Git: target.Git, Tag: target.Tag}
			builds, err := r.Reusable(target.ID, environment, reuseCandidates)
			if err != nil {
				return err
			}
			// Where the plan has it now, which an earlier build of a port
			// that moved since didn't read (reuse.Carry).
			if len(builds) > 0 {
				paths = append(paths, target.Directory)
			}
			for _, build := range builds {
				inputs, err := r.Inputs(build.Result.Inputs)
				if err != nil {
					return err
				}
				candidate := reuse.Candidate{Result: build.Result, Inputs: inputs, Origin: build.Execution}
				if target.Tag != nil {
					if candidate.Tagged, err = tagged(r, plans, build.Execution, target.ID); err != nil {
						return err
					}
				}
				targets[i].Earlier = append(targets[i].Earlier, candidate)
				paths = append(paths, reuse.Paths(inputs)...)
			}
		}
		return nil
	})
	return targets, paths, err
}

// tagged is the GitHub tag a target's archive was fetched by in an earlier
// build's check, with the commit it named then, from that check's plan;
// nil where the plan recorded none, or isn't kept. Plans are read once a
// check (plans).
func tagged(r store.Reader, plans map[model.RunID]model.Plan, origin model.GuestExecution, id model.TargetID) (*model.GitSource, error) {
	plan, ok := plans[origin.Run]
	if !ok {
		run, err := r.Run(origin.Run)
		if err == nil {
			plan, err = r.Plan(run.Plan)
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		plans[origin.Run] = plan
	}
	planned, _ := plan.In(origin.Environment)
	if tag, ok := planned.Tags[id]; ok {
		return &tag, nil
	}
	return nil, nil
}

// sayTagMoved says, of each target that builds, not reused, whose newest
// earlier build here that stands and was fetched by its GitHub tag finds
// the tag naming another commit now, that it builds again, and why
// (D-N5): the archive fetched now isn't the one that build checked, and
// dockhand checksums handles a stealth update. A target reused from an
// older build isn't said to build again: check-943 said jlv built again,
// then reused check-876's (rc7's G16).
func (d *driver) sayTagMoved(ctx context.Context, environment model.Environment, targets []reuse.Target, reused map[model.TargetID]reuse.Candidate) {
	for _, target := range targets {
		if _, ok := reused[target.ID]; ok {
			continue
		}
		i := slices.IndexFunc(target.Earlier, func(c reuse.Candidate) bool { return target.TagMoved(c) && d.plan.Tests.Stands(c.Result) })
		if i < 0 {
			continue
		}
		newest := target.Earlier[i]
		built := "an earlier check"
		_ = d.c.Store.View(ctx, d.c.Repository, func(r store.Reader) error {
			if run, err := r.Run(newest.Origin.Run); err == nil {
				built = run.Name()
			}
			return nil
		})
		d.emit(ctx, model.EventExecutionState, fmt.Sprintf("%s: %s: %s builds again: %s", d.run.Name(), DescribeEnvironment(environment), target.ID, TagMovedWords(*newest.Tagged, *target.Tag, built, target.Target.Name)))
	}
}

// TagMovedWords say that a GitHub tag names another commit now than when a
// check built a port, and what handles a stealth update: "v1.0.0 now names
// d4c0fdb, not 1d15bc8, as when check-41 built it; dockhand checksums foo
// handles a stealth update".
func TagMovedWords(then, now model.GitSource, built, port string) string {
	return fmt.Sprintf("%s now names %s, not %s, as when %s built it; dockhand checksums %s handles a stealth update", now.Ref, now.Commit.Short(), then.Commit.Short(), built, port)
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
		d.emit(ctx, model.EventExecutionState, fmt.Sprintf("%s: %s: %s builds again: its last build here passed, but can't be reused, since %s", d.run.Name(), DescribeEnvironment(environment), target.ID, newest.Inputs.NotReusable))
	}
}

// reusable chooses the targets that reuse an earlier build instead of
// building, before an environment's first attempt (reuse.Choose, decision
// 28): each target's newest earlier build that stands and read what it
// would read now, in the environment of its identity now. A reused target
// one that builds needs is installed from its kept archive, or builds. A
// check with Fresh reuses nothing, and nor does an environment that can't
// say what it is.
//
// A target whose build is carried over a change that builds nothing
// differently (reuse.Carry) has lint run again on its Portfile on this Mac
// where its own Portfile changed, and is reused only where lint finds no
// error; its result takes lint's lines, or keeps the earlier Portfile's,
// said so, where lint can't run here (carry).
func (d *driver) reusable(ctx context.Context, identity string, targets []reuse.Target, paths []string, revision model.Revision) (reuse.Choice, error) {
	if d.plan.Fresh || identity == "" || len(paths) == 0 {
		return reuse.Choice{}, nil
	}
	tree := revision.Source.Tree
	trees, err := reuse.Objects(ctx, d.c.Checkout, tree, paths)
	if err != nil {
		return reuse.Choice{}, err
	}
	available := func(c reuse.Candidate) bool {
		_, kept, err := d.c.Archives.Kept(ctx, c.Result.Archive)
		return err == nil && kept
	}
	judge := reuse.Judge{Blocks: d.blocks(ctx), Inert: d.inert(ctx, revision)}
	if judge.Inert != nil {
		if judge.Moved, err = d.moved(ctx, revision.Source, targets, trees); err != nil {
			return reuse.Choice{}, err
		}
	}
	// A target lint finds an error in builds, and what it needs with it:
	// its candidates are set aside, and the choice made again.
	considered := slices.Clone(targets)
	linted := map[model.TargetID]lintAnswer{}
	for {
		choice := reuse.Choose(considered, identity, trees, judge, d.plan.Tests.Stands, available)
		refused := d.carry(ctx, revision, considered, &choice, linted)
		if len(refused) == 0 {
			return choice, nil
		}
		for i := range considered {
			if found, ok := refused[considered[i].ID]; ok {
				considered[i].Earlier = nil
				d.emit(ctx, model.EventExecutionState, fmt.Sprintf("%s: %s builds again: lint found an error in its Portfile: %s", d.run.Name(), considered[i].ID, strings.Join(found, "; ")))
			}
		}
	}
}

// moved finds where each port an earlier build had active is now, by its
// name, where the directory it read is gone from the revision: a target
// where the plan has it, any other port as the revision's index has it.
// What it finds is added to trees, by directory (reuse.Objects).
func (d *driver) moved(ctx context.Context, source model.Source, targets []reuse.Target, trees map[string]model.ObjectID) (func(string) (string, bool), error) {
	found := map[string]string{}
	var lookups []string
	for _, target := range targets {
		for _, c := range target.Earlier {
			for _, port := range c.Inputs.Active {
				key := strings.ToLower(port.Name)
				if _, held := trees[port.Directory]; held || port.Directory == "" || slices.Contains(lookups, key) {
					continue
				}
				lookups = append(lookups, key)
				if i := slices.IndexFunc(d.plan.Targets, func(t model.PlanTarget) bool {
					return strings.EqualFold(t.Target.Name, port.Name) || strings.EqualFold(t.Target.Subport, port.Name)
				}); i >= 0 {
					found[key] = d.plan.Targets[i].Directory
					continue
				}
				if reader, _, err := d.c.ports(); err == nil && reader != nil {
					if directory, err := reader.Directory(ctx, source, port.Name); err == nil && directory != "" {
						found[key] = directory
					}
				}
			}
		}
	}
	var directories []string
	for _, directory := range found {
		if _, held := trees[directory]; !held && !slices.Contains(directories, directory) {
			directories = append(directories, directory)
		}
	}
	if len(directories) > 0 {
		more, err := reuse.Objects(ctx, d.c.Checkout, source.Tree, directories)
		if err != nil {
			return nil, err
		}
		maps.Copy(trees, more)
	}
	return func(port string) (string, bool) {
		directory, ok := found[strings.ToLower(port)]
		return directory, ok
	}, nil
}

// carry completes each result a choice carries over a change that builds
// nothing differently (choice.Carried): the check and commit it was built
// at, the ports judged, and, where the target's own Portfile changed,
// lint run again on it here, its warnings the result's (relint). It
// returns the targets lint found an error in, which aren't carried, each
// with lint's errors. Each target is linted once (linted).
func (d *driver) carry(ctx context.Context, revision model.Revision, targets []reuse.Target, choice *reuse.Choice, linted map[model.TargetID]lintAnswer) map[model.TargetID][]string {
	refused := map[model.TargetID][]string{}
	for _, target := range targets {
		ports, ok := choice.Carried[target.ID]
		c, reused := choice.Reused[target.ID]
		if !ok || !reused {
			continue
		}
		carried := &model.Carried{Ports: ports}
		_ = d.c.Store.View(ctx, d.c.Repository, func(r store.Reader) error {
			run, err := r.Run(c.Origin.Run)
			if err != nil {
				return err
			}
			carried.Check = run.Name()
			if built, err := r.Revision(run.Revision); err == nil {
				carried.Commit = built.Source.Commit
			}
			return nil
		})
		if slices.ContainsFunc(ports, func(p model.InertPort) bool { return p.Port == target.Target.Name && len(p.Options) > 0 }) {
			answer, ok := linted[target.ID]
			if !ok {
				answer = d.relint(ctx, revision, target)
				linted[target.ID] = answer
			}
			if len(answer.failed) > 0 {
				refused[target.ID] = answer.failed
				continue
			}
			if answer.kept != "" {
				carried.LintKept = answer.kept
			} else {
				carried.Linted = true
				c.Result.Lint, c.Result.Nitpicked = answer.lines, answer.nitpicked
			}
		}
		c.Result.Carried = carried
		choice.Reused[target.ID] = c
	}
	return refused
}

// lintAnswer is what lint run again on this Mac said of a target's
// Portfile (relint): its warnings, and whether it nitpicked; or why it
// couldn't run here, the earlier Portfile's lines standing; or the errors
// it found, which the guest's lint fails the target on.
type lintAnswer struct {
	lines     []string
	nitpicked bool
	kept      string
	failed    []string
}

// relint runs port lint on a target's Portfile as the revision holds it,
// on this Mac, as the guest runs it, with --nitpick for a port the branch
// adds. Where this Mac's reader can't lint, or lint couldn't finish, it
// says why.
func (d *driver) relint(ctx context.Context, revision model.Revision, target reuse.Target) lintAnswer {
	reader, _, err := d.c.ports()
	linter, ok := reader.(Linter)
	if err != nil || reader == nil || !ok {
		return lintAnswer{kept: "lint can't run on this Mac"}
	}
	port := target.Target.Name
	if target.Target.Subport != "" {
		port = target.Target.Subport
	}
	said, err := linter.Lint(ctx, revision.Source, target.Directory, port, target.Added)
	if err != nil {
		return lintAnswer{kept: "lint couldn't run on this Mac: " + err.Error()}
	}
	if len(said.Errors) > 0 {
		return lintAnswer{failed: said.Errors}
	}
	return lintAnswer{lines: append([]string{}, said.Warnings...), nitpicked: target.Added}
}

// blocks reads, of a port directory whose tree differs from the one a
// build read, which subports' literal blocks alone hold the difference
// (reuse.Blocks, portfile.BlockChanges): every entry but the Portfile the
// same object, and the Portfile's lines that differ each inside some
// subport's block, both versions read from their trees (portfiles). Anything it can't read says nothing, and
// the build reuses nothing, as before. Each pair of trees is read once.
func (d *driver) blocks(ctx context.Context) reuse.Blocks {
	type pair struct{ then, now model.ObjectID }
	type answer struct {
		subports []string
		ok       bool
	}
	read := map[pair]answer{}
	return func(_ string, then, now model.ObjectID) ([]string, bool) {
		key := pair{then, now}
		if a, ok := read[key]; ok {
			return a.subports, a.ok
		}
		var subports []string
		texts, ok := d.portfiles(ctx, then, now)
		if ok {
			var all bool
			subports, all = portfile.BlockChanges(texts[0], texts[1])
			ok = !all
		}
		read[key] = answer{subports, ok}
		return subports, ok
	}
}

// inert reads, of a port directory whose tree differs from the one a build
// read, the inert options alone that tell them apart (reuse.Inert): every
// entry but the Portfile the same object, the Portfile's changed lines
// each an inert option's (portfile.InertChanges), and the change records
// of the build's revision and of this one, made where they aren't yet,
// finding nothing else moved (fidelity.InertBetween); for a port both
// revisions add, which their records can't confirm, the directory's
// evaluations at the two revisions (Hooks.Family), the build's at the
// directory it read, finding the same. Anything it can't read or
// evaluate says nothing, and the build reuses nothing, as before. None
// where nothing gives change records. Each judgment, each revision's
// records, and each evaluation is read once.
func (d *driver) inert(ctx context.Context, revision model.Revision) reuse.Inert {
	if d.c.Hooks == nil {
		return nil
	}
	type key struct {
		origin   model.RunID
		from, at string
		then     model.ObjectID
		now      model.ObjectID
	}
	type answer struct {
		options []string
		ok      bool
	}
	read := map[key]answer{}
	records := map[model.RevisionID]map[string]model.ChangeRecord{}
	type family struct {
		snapshot macports.Snapshot
		err      error
	}
	families := map[[2]string]family{}
	familyOf := func(r model.Revision, directory string) (macports.Snapshot, error) {
		k := [2]string{string(r.ID), directory}
		if found, ok := families[k]; ok {
			return found.snapshot, found.err
		}
		snapshot, err := d.c.Hooks.Family(ctx, r, directory)
		families[k] = family{snapshot, err}
		return snapshot, err
	}
	recordsOf := func(r model.Revision) map[string]model.ChangeRecord {
		if found, ok := records[r.ID]; ok {
			return found
		}
		found, err := d.c.Hooks.Changes(ctx, r)
		if err != nil {
			found = nil
		}
		records[r.ID] = found
		return found
	}
	return func(origin model.RunID, from string, then model.ObjectID, at string, now model.ObjectID) ([]string, bool) {
		k := key{origin, from, at, then, now}
		if a, ok := read[k]; ok {
			return a.options, a.ok
		}
		var options []string
		texts, ok := d.portfiles(ctx, then, now)
		if ok {
			options, ok = portfile.InertChanges(texts[0], texts[1])
		}
		if ok {
			var built model.Revision
			err := d.c.Store.View(ctx, d.c.Repository, func(r store.Reader) error {
				run, err := r.Run(origin)
				if err == nil {
					built, err = r.Revision(run.Revision)
				}
				return err
			})
			thenRecord, had := recordsOf(built)[from]
			nowRecord, has := recordsOf(revision)[at]
			evaluations := func() (macports.Snapshot, macports.Snapshot, error) {
				then, err := familyOf(built, from)
				if err != nil {
					return then, macports.Snapshot{}, err
				}
				now, err := familyOf(revision, at)
				return then, now, err
			}
			ok = err == nil && had && has && fidelity.InertBetween(thenRecord, nowRecord, evaluations)
		}
		read[k] = answer{options, ok}
		return options, ok
	}
}

// portfiles are the Portfiles of two trees of a port directory whose every
// other entry is the same object, read from the trees, so nothing is
// recorded or migrated for them; not ok where anything else differs, or
// either can't be read.
func (d *driver) portfiles(ctx context.Context, then, now model.ObjectID) ([2][]byte, bool) {
	var texts [2][]byte
	before, err := d.c.Checkout.ReadTree(ctx, string(then))
	if err != nil {
		return texts, false
	}
	after, err := d.c.Checkout.ReadTree(ctx, string(now))
	if err != nil || len(before) != len(after) {
		return texts, false
	}
	found := false
	for i := range before {
		switch {
		case before[i].Name != after[i].Name || before[i].Mode != after[i].Mode:
			return texts, false
		case before[i].Name == "Portfile":
			found = true
		case before[i].Object != after[i].Object:
			return texts, false
		}
	}
	if !found {
		return texts, false
	}
	for i, tree := range []model.ObjectID{then, now} {
		files, err := d.c.Checkout.ReadFiles(ctx, string(tree), []string{"Portfile"})
		if err != nil || files["Portfile"] == nil {
			return texts, false
		}
		texts[i] = files["Portfile"]
	}
	return texts, true
}

// installs are the kept archives the guest installs targets it doesn't
// build from, for the targets it builds that need them (reuse.Needs):
// each a target with a passed result here, reused or finished in an
// earlier attempt. One whose archive isn't kept is left to MacPorts. Each
// goes with the PortGroups its build loaded, where its result's inputs
// name them (builtWith).
func (d *driver) installs(ctx context.Context, environment model.Environment, building []reuse.Target, results map[model.TargetID]model.TargetResult) ([]buildenv.Archive, builtWith, error) {
	planned, _ := d.plan.In(environment)
	var installs []buildenv.Archive
	var inputs []string
	for _, target := range building {
		for _, need := range reuse.Needs(target, planned.Order) {
			if slices.ContainsFunc(building, func(t reuse.Target) bool { return t.ID == need }) || slices.ContainsFunc(installs, func(a buildenv.Archive) bool { return a.Target == need }) {
				continue
			}
			result, ok := results[need]
			if !ok || result.Outcome != model.OutcomePassed || result.Archive == "" {
				continue
			}
			archive, kept, err := d.c.Archives.Kept(ctx, result.Archive)
			if err != nil {
				return nil, nil, err
			}
			if !kept {
				continue
			}
			// A kept archive changed since it was kept, by a byte or more,
			// isn't given to the guest: MacPorts builds the target instead,
			// and the check says why (prime-time D-T1). Signing would refuse
			// it too, and fail the check, where a build does the work.
			if _, digest, err := sha256File(d.c.Archives.Path(archive.Digest)); err != nil || digest != archive.Digest {
				if err := d.altered(ctx, string(need), archive.Digest, "the guest builds it instead"); err != nil {
					return nil, nil, err
				}
				continue
			}
			planned, _ := d.plan.Target(need)
			port := planned.Target.Name
			if planned.Target.Subport != "" {
				port = planned.Target.Subport
			}
			installs = append(installs, buildenv.Archive{Target: need, Port: port, Name: archive.Name, Digest: archive.Digest, Path: d.c.Archives.Path(archive.Digest)})
			inputs = append(inputs, result.Inputs)
		}
	}
	if len(installs) == 0 {
		return nil, nil, nil
	}
	// A result recorded before any provider said which PortGroups its
	// build loaded, or whose build was taken to have read all of
	// _resources, names none.
	groups := builtWith{}
	if err := d.c.Store.View(ctx, d.c.Repository, func(r store.Reader) error {
		for i, archive := range installs {
			if inputs[i] == "" {
				continue
			}
			read, err := r.Inputs(inputs[i])
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if read.ResourcesRead != nil && read.ResourcesRead.PortGroups != nil {
				groups[archive.Digest] = read.ResourcesRead.PortGroups
			}
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return installs, groups, nil
}

// builtWith is what the kept archives a check offers its guest were built
// with, by their digests: the PortGroups each build loaded, where a guest
// built it for a check and said which (Codex's review of 03b380f1, F1). A
// target that installed one read what that build made, so they are its
// inputs as they would be had it built the port itself (build.read).
// MacPorts' archives, and those kept before any guest said, have none,
// and a target that installed one stays keyed by its archive alone.
type builtWith map[string]map[string]model.ObjectID

// recordReuse records the execution that reuses every target's earlier
// build: one that ran nothing, in the environment as it is now, reporting
// what the newest build it reuses found of the environment. The provider
// isn't started at all.
func (d *driver) recordReuse(ctx context.Context, environment model.Environment, identity string, remaining []buildenv.Target, chosen map[model.TargetID]reuse.Candidate) error {
	now := d.c.Now()
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
		message := fmt.Sprintf("%s: %s: reusing results from %s for all targets because their build inputs are unchanged; no builds are needed", d.run.Name(), DescribeEnvironment(environment), strings.Join(checks, ", "))
		if lines := carriedLines(d.run.Name(), ordered); len(lines) > 0 {
			message = fmt.Sprintf("%s: %s: reusing results from %s for all targets; no builds are needed: %s", d.run.Name(), DescribeEnvironment(environment), strings.Join(checks, ", "), strings.Join(lines, "; "))
		}
		_, err = d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: model.EventExecutionState, Level: model.LevelInfo, Message: message})
		return err
	})
}

// reuses records, in the execution that builds the rest, the results of
// the targets that reuse an earlier build, before the provider starts.
func (b *build) reuses(remaining []buildenv.Target, chosen map[model.TargetID]reuse.Candidate) error {
	now := b.d.c.Now()
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
		message := fmt.Sprintf("%s: %s: reusing results for %s from %s because their build inputs are unchanged; building the remaining targets", b.d.run.Name(), DescribeEnvironment(b.execution.Environment), strings.Join(names, ", "), strings.Join(checks, ", "))
		if lines := carriedLines(b.d.run.Name(), ordered); len(lines) > 0 {
			message = fmt.Sprintf("%s: %s: reusing results for %s from %s; building the remaining targets: %s", b.d.run.Name(), DescribeEnvironment(b.execution.Environment), strings.Join(names, ", "), strings.Join(checks, ", "), strings.Join(lines, "; "))
		}
		_, err = b.d.session.Emit(tx, model.Event{Branch: b.d.run.Branch, Run: b.d.run.ID, Kind: model.EventExecutionState, Level: model.LevelInfo, Message: message})
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

// carriedLines say, of the chosen builds carried over a change that
// builds nothing differently (model.Carried), what changed since, a
// sentence for each earlier check, in the order the builds are
// (CarriedWords).
func carriedLines(run string, ordered []reuse.Candidate) []string {
	var checks []string
	byCheck := map[string][]model.Carried{}
	for _, c := range ordered {
		if c.Result.Carried == nil {
			continue
		}
		if _, ok := byCheck[c.Result.Carried.Check]; !ok {
			checks = append(checks, c.Result.Carried.Check)
		}
		byCheck[c.Result.Carried.Check] = append(byCheck[c.Result.Carried.Check], *c.Result.Carried)
	}
	var lines []string
	for _, check := range checks {
		lines = append(lines, CarriedWords(run, byCheck[check]))
	}
	return lines
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
// from (batch 90): the dependency closure of what the guest builds, at the
// check's revision, a target's direct dependencies as MacPorts evaluates
// it in the revision and theirs on from the port index (DependencyGraph),
// so a dependency's dependency is offered on a target's first check too.
// An earlier result's active ports aren't: gnome-voice's check was offered
// about 230, libadwaita, geoclue2, and gst-plugins-bad among them, which
// its version no longer needs (the gnome-voice session's notes,
// 2026-10-20). The guest's MacPorts takes one whose name is the archive it
// wants, and builds the rest as before, so rust and cargo, built from
// source once on a release MacPorts has no archives of them for, aren't
// built again by a later check of any port needing them. A port the plan
// builds is the branch's, and none of its are given. One changed since it
// was kept isn't given, nor one a guest built from source with PortGroups
// the check's revision holds otherwise: it is the build of what the ports
// tree no longer says, which a target voided by that change would be
// built again against (prioritization's ruling, 2026-11-01).
// Each goes with the PortGroups a guest built it with, where one did
// (builtWith).
func (d *driver) dependencyInstalls(ctx context.Context, environment model.Environment, source model.Source, building []reuse.Target) ([]buildenv.Archive, builtWith) {
	var ports []string
	var queue []string
	seen := map[string]bool{}
	add := func(port string) {
		key := strings.ToLower(port)
		if port == "" || seen[key] {
			return
		}
		seen[key] = true
		queue = append(queue, key)
		for _, target := range d.plan.Targets {
			if strings.EqualFold(target.Target.Name, port) || strings.EqualFold(target.Target.Subport, port) {
				return
			}
		}
		ports = append(ports, port)
	}
	reader, graphs, err := d.c.ports()
	if err != nil || reader == nil {
		return nil, nil
	}
	for _, target := range building {
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
	// Their dependencies on, from the revision's port index; where it
	// can't be read, the direct dependencies alone are offered.
	if graphs != nil && len(queue) > 0 {
		if graph, err := graphs.DependencyGraph(ctx, source); err == nil {
			for len(queue) > 0 {
				next := queue[0]
				queue = queue[1:]
				for _, dependency := range graph[next] {
					add(dependency)
				}
			}
		}
	}
	// The archives' records are read with the dependencies', in one
	// view (the SQL study's SQL-O5).
	var kept []model.DependencyArchive
	recorded := map[string]model.Archive{}
	if err := d.c.Store.View(ctx, d.c.Repository, func(r store.Reader) error {
		var err error
		if kept, err = r.DependencyArchives(environment, ports); err != nil {
			return err
		}
		for _, archive := range kept {
			if a, err := r.Archive(archive.Digest); err == nil {
				recorded[archive.Digest] = a
			} else if !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, nil
	}
	// The revision's PortGroups, where a kept archive says what it was
	// built with; where they can't be read, none such is offered.
	var objects map[string]model.ObjectID
	if slices.ContainsFunc(kept, func(a model.DependencyArchive) bool { return a.PortGroups != nil }) {
		objects, err = reuse.Objects(ctx, d.c.Checkout, source.Tree, []string{macports.ResourcesDirectory})
		if err != nil {
			objects = nil
		}
	}
	var installs []buildenv.Archive
	groups := builtWith{}
	var missing []model.DependencyArchive
	var stale []staleArchive
	for _, archive := range kept {
		// One whose file is gone is said, and its record dropped, where
		// each check skipped it again in silence (Codex's gcc15 group
		// update, 2026-10-07).
		a, ok := recorded[archive.Digest]
		if info, err := os.Stat(d.c.Archives.Path(archive.Digest)); !ok || err != nil || info.Size() != a.Size {
			missing = append(missing, archive)
			continue
		}
		// One built with PortGroups the revision holds otherwise isn't
		// read through for its digest, since it isn't offered.
		if archive.PortGroups != nil {
			if objects == nil {
				continue
			}
			if changed := reuse.PortGroupChanged(archive.PortGroups, objects); changed != "" {
				stale = append(stale, staleArchive{port: archive.Port, changed: changed})
				continue
			}
		}
		if _, digest, err := sha256File(d.c.Archives.Path(archive.Digest)); err != nil || digest != archive.Digest {
			// Said, as a target's is, where it was skipped in silence (the
			// rc8 full stage's D-T1). What can't be said leaves the guest
			// to fetch the dependency as before.
			_ = d.altered(ctx, archive.Port, archive.Digest, "the guest gets it as MacPorts would, from its archives or a build")
			continue
		}
		installs = append(installs, buildenv.Archive{Port: archive.Port, Name: archive.Name, Digest: archive.Digest, Path: d.c.Archives.Path(archive.Digest)})
		if archive.PortGroups != nil {
			groups[archive.Digest] = archive.PortGroups
		}
	}
	if len(missing) > 0 {
		_ = d.gone(ctx, missing)
	}
	// Those not offered for what changed since are said once, for the
	// ports no other kept archive of is offered for.
	stale = slices.DeleteFunc(stale, func(a staleArchive) bool {
		return slices.ContainsFunc(installs, func(i buildenv.Archive) bool { return strings.EqualFold(i.Port, a.port) })
	})
	if len(stale) > 0 {
		d.emit(ctx, model.EventProgress, staleWords(d.run.Name(), stale))
	}
	return installs, groups
}

// staleArchive is a dependency's kept archive a guest built with a
// PortGroup the check's revision holds otherwise: its port, and what
// changed, "the cmake 1.1 PortGroup".
type staleArchive struct{ port, changed string }

// staleWords say the kept archives not offered since a PortGroup their
// builds loaded changed: one by its port and the PortGroup, several by
// their count and ports.
func staleWords(run string, archives []staleArchive) string {
	if len(archives) == 1 {
		return fmt.Sprintf("%s: the saved archive for %s was built before %s changed, so the build environment will use a MacPorts archive or build it from source", run, archives[0].port, archives[0].changed)
	}
	var ports []string
	for _, archive := range archives {
		if !slices.Contains(ports, archive.port) {
			ports = append(ports, archive.port)
		}
	}
	return fmt.Sprintf("%s: %d saved archives, of %s, were built before PortGroups they loaded changed, so the build environment will use MacPorts archives or build them from source", run, len(archives), prose.And(ports))
}

// gone says the dependencies' kept archives whose files are missing, or
// aren't the size they were kept at, once, and drops their records, so no
// later check meets them: a DOCKHAND_DB copy without its archives
// directory said each of 58 on a line of its own (the Prime-time session
// at 42d3891a).
func (d *driver) gone(ctx context.Context, archives []model.DependencyArchive) error {
	return d.fenced(ctx, func(tx store.Tx) error {
		for _, archive := range archives {
			if err := tx.ForgetDependencyArchive(archive); err != nil {
				return err
			}
		}
		_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: model.EventArchiveGone, Level: model.LevelInfo, Message: goneWords(d.run.Name(), archives)})
		return err
	})
}

// goneWords say the kept archives found missing: one by its port and
// digest, several by their count and ports.
func goneWords(run string, archives []model.DependencyArchive) string {
	if len(archives) == 1 {
		return fmt.Sprintf("%s: the saved archive for %s (%s) is missing. Removing its record; the build environment will use a MacPorts archive or build it from source", run, archives[0].Port, archives[0].Digest)
	}
	var ports []string
	for _, archive := range archives {
		if !slices.Contains(ports, archive.Port) {
			ports = append(ports, archive.Port)
		}
	}
	return fmt.Sprintf("%s: %d saved archives are missing, of %s. Removing their records; the build environment will use MacPorts archives or build them from source", run, len(archives), prose.And(ports))
}

// altered says a kept archive that isn't the one it was kept as, by a
// byte or more, as tampering or a disk's fault leaves one, and sets the
// file aside beside it, as <file>.altered, so no later check meets it
// again, and what it was is there to look at. Its record goes as an
// archive's whose file is gone does, at cleanup. keptArchive, which
// compares only the size, is the cheap test reuse and cleanup make; the
// digest is read here, before a guest is given one (prime-time D-T1).
func (d *driver) altered(ctx context.Context, port, digest, instead string) error {
	path := d.c.Archives.Path(digest)
	aside := ""
	if os.Rename(path, path+".altered") == nil {
		aside = "; it's set aside as " + path + ".altered"
	}
	return d.fenced(ctx, func(tx store.Tx) error {
		_, err := d.session.Emit(tx, model.Event{Branch: d.run.Branch, Run: d.run.ID, Kind: model.EventArchiveAltered, Level: model.LevelInfo,
			Message: fmt.Sprintf("%s: the archive kept of %s isn't the one it was kept as, %s, so %s%s", d.run.Name(), port, digest, instead, aside)})
		return err
	})
}
