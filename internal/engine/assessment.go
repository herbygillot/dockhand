package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
	"github.com/herbygillot/dockhand/internal/store"
)

// An assessment is a revision's (the assessment design, D): each port a
// revision's files change is assessed against the revision's captured
// base, whoever changed it, so its net change is what's assessed, never
// the edits that made it. A license changed and changed back holds
// nothing, and a hand edit after a clean comparison is assessed again.
//
// Collecting one fetches and evaluates, so only commands acting on a
// request collect: update, a check's driver, submit, and serve. Status
// reads what's recorded.

// revisionAssessments are what upstream's change means for each port a
// revision's files change against the base it was captured on: those
// recorded that still apply, and, where collect, those made now for the
// rest, which are recorded. What collecting one couldn't do is its
// comparison's Problem, never an error: the assessment is incomplete, and
// holds as what couldn't be checked does. One recorded incomplete is kept
// for its files, as an update's comparison always was; their next version
// is assessed afresh.
func (e *Engine) revisionAssessments(ctx context.Context, branch model.BranchID, base, tree model.ObjectID, collect bool) ([]model.Assessment, error) {
	trees, err := e.Repo.CommitTrees(ctx, []string{string(base)})
	if err != nil {
		return nil, err
	}
	baseTree := model.ObjectID(trees[string(base)])
	changed, err := e.Repo.ChangedPaths(ctx, string(baseTree), string(tree))
	if err != nil {
		return nil, err
	}
	var recorded []model.Assessment
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		all, err := r.Assessments(branch)
		for _, a := range all {
			if a.Tree == tree && a.Base == base && a.Policy == assess.Policy {
				recorded = append(recorded, a)
			}
		}
		return err
	}); err != nil {
		return nil, err
	}
	have := func(port string) (model.Assessment, bool) {
		i := slices.IndexFunc(recorded, func(a model.Assessment) bool { return a.Port == port })
		if i < 0 {
			return model.Assessment{}, false
		}
		return recorded[i], true
	}
	if !collect {
		return recorded, nil
	}
	reader, err := e.portReader()
	if err != nil {
		return nil, err
	}
	planner, err := e.archivePlanner()
	if err != nil {
		// Only an engine given no evaluator can't plan archives: it
		// collects nothing, and reads what's recorded.
		return recorded, nil
	}
	sources := [2]model.Source{{Commit: base, Tree: baseTree, Base: base}, {Tree: tree, Base: base}}
	var found, made []model.Assessment
	for _, directory := range ScopeOf(changed).Ports {
		exists := [2]bool{}
		for i, source := range sources {
			file, _, err := e.Repo.File(ctx, string(source.Tree), directory+"/Portfile")
			if err != nil {
				return nil, err
			}
			exists[i] = file.Exists
		}
		if !exists[1] {
			// A port removed or moved away is assessed where it now is,
			// if anywhere.
			continue
		}
		ports, err := reader.Ports(ctx, sources[1], directory, model.Environment{}, nil)
		if err != nil && slices.ContainsFunc(recorded, func(a model.Assessment) bool { return a.Directory == directory }) {
			// What was recorded of it stands, as an update recorded it.
			for _, a := range recorded {
				if a.Directory == directory {
					found = append(found, a)
				}
			}
			continue
		}
		if err != nil {
			made = append(made, model.Assessment{Branch: branch, Tree: tree, Base: base, Port: directory, Directory: directory, Policy: assess.Policy, At: e.now(),
				Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{}, Problem: "its ports couldn't be read: " + err.Error()}})
			continue
		}
		for _, port := range ports {
			if a, ok := have(port.Name); ok {
				found = append(found, a)
				continue
			}
			made = append(made, model.Assessment{Branch: branch, Tree: tree, Base: base, Port: port.Name, Directory: directory, Policy: assess.Policy, At: e.now(),
				Comparison: e.assessPort(ctx, planner, sources, directory, port.Name, exists[0])})
		}
	}
	if len(made) > 0 {
		if err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
			for _, a := range made {
				if err := tx.RecordAssessment(a); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return append(found, made...), nil
}

// revisionComparisons are a revision's assessments, by port, as a
// submission and status show them.
func (e *Engine) revisionComparisons(ctx context.Context, branch model.Branch, tree model.ObjectID, collect bool) ([]PortComparison, error) {
	assessments, err := e.revisionAssessments(ctx, branch.ID, branch.Base, tree, collect)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(assessments, func(a, b model.Assessment) int { return cmp.Compare(a.Port, b.Port) })
	var comparisons []PortComparison
	for _, a := range assessments {
		comparisons = append(comparisons, PortComparison{Port: a.Port, Comparison: a.Comparison})
	}
	return comparisons, nil
}

// archivePlanner is what says what a port fetches: MacPorts' evaluator,
// unless one was given.
func (e *Engine) archivePlanner() (ArchivePlanner, error) {
	if e.ArchivePlanner != nil {
		return e.ArchivePlanner, nil
	}
	reader, err := e.portReader()
	if err != nil {
		return nil, err
	}
	planner, ok := reader.(ArchivePlanner)
	if !ok {
		return nil, errors.New("assessing a revision needs MacPorts' evaluator")
	}
	return planner, nil
}

// assessPort assesses one port of a directory at the revision against the
// base, as this Mac's context fetches it. A port new to the directory has
// no base, and its candidate is assessed alone, with nothing said of a
// missing old archive; a port that fetches no upstream source has nothing
// to compare, and says so.
func (e *Engine) assessPort(ctx context.Context, planner ArchivePlanner, sources [2]model.Source, directory, name string, hadBase bool) model.UpstreamComparison {
	var infos [2]macports.PortInfo
	var plans [2][]macports.Distfile
	problem := ""
	fetches := true
	for side, source := range sources {
		if side == 0 && !hadBase {
			continue
		}
		info, plan, err := planner.ArchivePlan(ctx, source, directory, name)
		switch {
		case errors.Is(err, ErrNoArchives):
			if side == 1 {
				fetches = false
			}
		case errors.Is(err, ErrNoPort) && side == 0:
			// A subport new to the directory has no base to evaluate.
			hadBase = false
			continue
		case err != nil && side == 0:
			problem = "the base couldn't be evaluated: " + err.Error()
		case err != nil:
			problem = err.Error()
		}
		infos[side], plans[side] = info, plan
	}
	input := assess.Input{Base: infos[0], Port: infos[1], Versions: sourcecompare.Versions{Old: infos[0].Version, New: infos[1].Version}}
	var coverage []model.Coverage
	switch {
	case problem != "":
	case infos[1].GitFetched():
		// Assessing a Git-fetched port through its forge's archive of each
		// commit is batch 20's source identity's to make possible; until
		// then, what couldn't be checked holds (D4).
		problem = "a Git-fetched port's upstream isn't assessed yet"
	case !fetches:
		coverage = append(coverage, model.Coverage{Path: directory, Relevance: "unknown", Treatment: "inspected", Reason: name + " fetches no upstream source, so there's nothing to compare"})
	default:
		input.Pairs, problem = e.readPlans(ctx, infos, plans, hadBase)
	}
	if problem == "" && len(input.Pairs) > 0 {
		input.Observed = e.observeProviders(ctx, input, sources)
	}
	comparison := assess.Assess(input)
	comparison.Problem = problem
	comparison.Coverage = append(comparison.Coverage, coverage...)
	return comparison
}

// readPlans reads the archives each version's fetch plan names, paired:
// an archive both name alike is itself on each side, the rest are paired
// in the order the plans name them, and one only the revision names is
// read beside nothing, as new. A reading kept for an archive's content,
// by the sha256 its Portfile declares, stands without fetching it again;
// the rest are fetched as the Portfile's checksums declare them, and
// kept. What couldn't be fetched or read is the problem.
func (e *Engine) readPlans(ctx context.Context, infos [2]macports.PortInfo, plans [2][]macports.Distfile, hadBase bool) ([]assess.Pair, string) {
	type side struct {
		info     macports.PortInfo
		plan     []macports.Distfile
		declared map[string]portfile.Checksum
		spec     project.Spec
		readings map[string]project.Reading
	}
	var sides [2]side
	for i := range sides {
		if i == 0 && !hadBase {
			continue
		}
		sides[i] = side{info: infos[i], plan: plans[i], declared: archives.Declared(infos[i].Options["checksums"]),
			spec: project.Spec{Subdirectory: macports.SourceSubdirectory(infos[i].Options["worksrcdir"])}, readings: map[string]project.Reading{}}
	}
	digest := func(s side, name string) string {
		if sum, ok := s.declared[name]; ok {
			return sum.SHA256
		}
		if sum, ok := s.declared[""]; ok && len(s.plan) == 1 {
			return sum.SHA256
		}
		return ""
	}
	directory, err := scratch.Dir("assess-")
	if err != nil {
		return nil, err.Error()
	}
	defer os.RemoveAll(directory)
	cache := e.readings()
	for i, s := range sides {
		var missing []macports.Distfile
		for _, file := range s.plan {
			if reading, ok := cache.Kept(digest(s, file.Name), s.spec); ok {
				s.readings[file.Name] = reading
				continue
			}
			missing = append(missing, file)
		}
		if len(missing) == 0 {
			continue
		}
		into, err := os.MkdirTemp(directory, "")
		if err != nil {
			return nil, err.Error()
		}
		fetched, err := fetchPlanned(ctx, archives.Client{Mirror: archives.MacPortsMirror}.Store(into), s.info, missing)
		if err != nil {
			which := "the revision's"
			if i == 0 {
				which = "the base's"
			}
			return nil, fmt.Sprintf("%s archives couldn't be fetched: %v", which, err)
		}
		for _, archive := range fetched {
			reading, err := cache.Read(ctx, archive.Path, archive.Sum.SHA256, s.spec)
			if err != nil {
				return nil, fmt.Sprintf("reading %s: %v", archive.Name, err)
			}
			s.readings[archive.Name] = reading
		}
	}
	// Pair them: alike by name first, then in the order the plans name the
	// rest.
	var pairs []assess.Pair
	var before, after []string
	for _, file := range sides[1].plan {
		after = append(after, file.Name)
	}
	for _, file := range sides[0].plan {
		if i := slices.Index(after, file.Name); i >= 0 {
			pairs = append(pairs, assess.Pair{Archive: file.Name, Before: sides[0].readings[file.Name], After: sides[1].readings[file.Name]})
			after = slices.Delete(after, i, i+1)
			continue
		}
		before = append(before, file.Name)
	}
	for i, name := range after {
		pair := assess.Pair{Archive: name, Before: project.Reading{Layout: project.Enclosed, Files: map[string]project.File{}}, After: sides[1].readings[name]}
		if i < len(before) {
			pair.Before = sides[0].readings[before[i]]
		}
		pairs = append(pairs, pair)
	}
	return pairs, ""
}
