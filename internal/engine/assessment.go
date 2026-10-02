package engine

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
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
		revision, err := r.Assessments(store.AssessmentFilter{Branch: branch, Tree: tree, Base: base})
		for _, a := range revision {
			if a.Policy == assess.Policy {
				recorded = append(recorded, a)
			}
		}
		return err
	}); err != nil {
		return nil, err
	}
	if !collect {
		return recorded, nil
	}
	found, made, err := e.makeAssessments(ctx, branch, base, baseTree, tree, changed, recorded)
	switch {
	case errors.Is(err, errNoPlanner):
		// Only an engine given no evaluator can't plan archives: it
		// collects nothing, and reads what's recorded.
		return recorded, nil
	case err != nil:
		return nil, err
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
		return nil, errNoPlanner
	}
	return planner, nil
}

// makeAssessments makes the assessments of each port a revision's changed
// paths change, against its base, recording none: those recorded that
// apply are found, and the rest made, an incomplete one made again where
// another try may meet what kept it so; errNoPlanner where the engine,
// given no evaluator, can't plan archives. Review assesses a pull request
// this way, which no branch records.
func (e *Engine) makeAssessments(ctx context.Context, branch model.BranchID, base, baseTree, tree model.ObjectID, changed []string, recorded []model.Assessment) (found, made []model.Assessment, err error) {
	have := func(port string) (model.Assessment, bool) {
		i := slices.IndexFunc(recorded, func(a model.Assessment) bool { return a.Port == port })
		if i < 0 {
			return model.Assessment{}, false
		}
		return recorded[i], true
	}
	reader, err := e.portReader()
	if err != nil {
		return nil, nil, err
	}
	planner, err := e.archivePlanner()
	if err != nil {
		return nil, nil, err
	}
	sources := [2]model.Source{{Commit: base, Tree: baseTree, Base: base}, {Tree: tree, Base: base}}
	for _, directory := range macports.ScopeOf(changed).Ports {
		exists := [2]bool{}
		for i, source := range sources {
			file, _, err := e.Repo.File(ctx, string(source.Tree), directory+"/Portfile")
			if err != nil {
				return nil, nil, err
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
			// One recorded incomplete for what another try may not meet, a
			// network's failure or a forge's rate limit, is tried again.
			if a, ok := have(port.Name); ok && !a.Comparison.Transient {
				// Said, since whether one was made again or reused wasn't
				// (the rust and cargo run).
				progress.VerboseReport(ctx, "%s: the assessment recorded for these files under policy %d stands", port.Name, a.Policy)
				found = append(found, a)
				continue
			}
			made = append(made, model.Assessment{Branch: branch, Tree: tree, Base: base, Port: port.Name, Directory: directory, Policy: assess.Policy, At: e.now(),
				Comparison: e.assessPort(ctx, planner, sources, directory, port.Name, exists[0])})
		}
	}
	return found, made, nil
}

// errNoPlanner is an engine that can't plan archives, given no evaluator.
var errNoPlanner = errors.New("assessing a revision needs MacPorts' evaluator")

// assessPort assesses one port of a directory at the revision against the
// base, as this Mac's context fetches it. A port new to the directory has
// no base, and its candidate is assessed alone, with nothing said of a
// missing old archive; a port that fetches no upstream source has nothing
// to compare, and says so.
func (e *Engine) assessPort(ctx context.Context, planner ArchivePlanner, sources [2]model.Source, directory, name string, hadBase bool) model.UpstreamComparison {
	var infos [2]macports.PortInfo
	var plans [2][]macports.Distfile
	problem := ""
	// again is a problem another try may not meet.
	again := false
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
	input := assess.Input{Base: infos[0], Port: infos[1], Versions: sourcecompare.Versions{Old: infos[0].Version, New: infos[1].Version}, New: !hadBase}
	var coverage []model.Coverage
	switch {
	case problem != "":
	case infos[1].GitFetched():
		input.Pairs, coverage, problem, again = e.readCommits(ctx, infos, hadBase)
	case !fetches:
		coverage = append(coverage, model.Coverage{Path: directory, Relevance: "unknown", Treatment: "inspected", Policy: notCompared, Reason: name + " fetches no upstream source, so there's nothing to compare"})
	default:
		scratchDirectory, err := scratch.Dir("assess-")
		if err != nil {
			problem = err.Error()
			break
		}
		defer os.RemoveAll(scratchDirectory)
		var fetched map[string]string
		input.Pairs, input.Unpaired, fetched, problem, again = e.readPlans(ctx, infos, plans, hadBase, scratchDirectory)
		if problem == "" {
			input.Patches = e.patchesFor(ctx, patchRequest{infos: infos, sources: sources, portdir: directory, plan: plans[1], fetched: fetched, scratch: scratchDirectory})
		}
	}
	comparison := e.collect(ctx, input, sources, directory+"/Portfile", problem == "")
	comparison.Problem, comparison.Transient = problem, again && problem != ""
	if infos[1].GitFetched() && problem == "" && len(input.Pairs) == 1 {
		// What a Git-fetched port's assessment read is the commit its
		// git.branch named then, which a later check may not build.
		comparison.Commit = input.Pairs[0].Archive
	}
	comparison.Coverage = append(comparison.Coverage, coverage...)
	return comparison
}

// collect is the one place an assessment is made from what was gathered
// for it, by update and by a revision's assessment alike (the
// architecture review's finding 1): the candidate's Portfile read at its
// path, the providers its requirements name observed where its archives
// were read, and the rules applied.
func (e *Engine) collect(ctx context.Context, input assess.Input, trees [2]model.Source, portfile string, observe bool) model.UpstreamComparison {
	if file, data, err := e.Repo.File(ctx, string(trees[1].Tree), portfile); err == nil && file.Exists {
		input.Portfile = data
	}
	if observe && len(input.Pairs) > 0 {
		input.Observed = e.observeProviders(ctx, input, trees)
	}
	return assess.Assess(input)
}

// patchRequest is what checking a revision's patches reads: each version's
// port, the trees its patch files are in, the port's directory, and the
// candidate's archives, fetched by name where fetched has them, else from
// its plan into scratch. checked are the candidate's own patches an
// update's preparation already checked against those archives, which
// aren't checked again; nil where none were.
type patchRequest struct {
	infos   [2]macports.PortInfo
	sources [2]model.Source
	portdir string
	plan    []macports.Distfile
	fetched map[string]string
	scratch string
	checked []patchcheck.Result
}

// patchesFor checks the revision's patches, and those the base applied that
// it drops, against the revision's archives (patchcheck.Port), for update
// and a revision's assessment alike: update's assessment had none, and
// was kept as the revision's whole one, so a later look reused it without
// them (the architecture review's finding 1). Each patch is read from the
// port's files in its own version's tree, where filespath names below the
// port's directory; one the check can't reach is unchecked, with why. None
// where neither version declares patches.
func (e *Engine) patchesFor(ctx context.Context, request patchRequest) []assess.Patch {
	infos, sources, portdir := request.infos, request.sources, request.portdir
	own, ownErr := infos[1].Patchfiles()
	had, hadErr := infos[0].Patchfiles()
	var dropped []string
	for _, name := range had {
		if !slices.Contains(own, name) {
			dropped = append(dropped, name)
		}
	}
	if len(own)+len(dropped) == 0 {
		return nil
	}
	var found []assess.Patch
	unchecked := func(why string) []assess.Patch {
		for _, name := range own {
			found = append(found, assess.Patch{Result: patchcheck.Result{Name: name, Detail: why}})
		}
		for _, name := range dropped {
			found = append(found, assess.Patch{Result: patchcheck.Result{Name: name, Detail: why}, Dropped: true})
		}
		return found
	}
	if err := errors.Join(ownErr, hadErr); err != nil {
		return unchecked("patchfiles couldn't be read: " + err.Error())
	}
	// What the update's preparation checked stands; only what it didn't
	// check, the patches the candidate drops, is checked here.
	checking := own
	if request.checked != nil {
		for _, name := range own {
			at := slices.IndexFunc(request.checked, func(r patchcheck.Result) bool { return r.Name == name })
			if at < 0 {
				found = append(found, assess.Patch{Result: patchcheck.Result{Name: name, Detail: "the update didn't check it"}})
				continue
			}
			found = append(found, assess.Patch{Result: request.checked[at]})
		}
		if checking = nil; len(dropped) == 0 {
			return found
		}
		own = nil
	}
	var paths, missing []string
	var missed []macports.Distfile
	if request.plan == nil {
		for _, name := range slices.Sorted(maps.Keys(request.fetched)) {
			paths = append(paths, request.fetched[name])
		}
	}
	for _, file := range request.plan {
		if path, ok := request.fetched[file.Name]; ok {
			paths = append(paths, path)
		} else {
			missed = append(missed, file)
			missing = append(missing, file.Name)
		}
	}
	if len(missed) > 0 {
		into, err := os.MkdirTemp(request.scratch, "")
		if err != nil {
			return unchecked(err.Error())
		}
		archives, err := fetchPlanned(ctx, distfetch.Client{Mirror: e.mirror()}.Store(into), infos[1], missed)
		if err != nil {
			return unchecked(fmt.Sprintf("%s couldn't be fetched to check them against: %v", strings.Join(missing, ", "), err))
		}
		for _, archive := range archives {
			paths = append(paths, archive.Path)
		}
	}
	own = checking
	var patches []patchcheck.Patch
	read := func(side int, name string) ([]byte, string) {
		where, ok := infos[side].FilesPath(portdir, name)
		if !ok {
			return nil, "its filespath isn't below the port's directory"
		}
		file, data, err := e.Repo.File(ctx, string(sources[side].Tree), where)
		switch {
		case err != nil:
			return nil, err.Error()
		case !file.Exists:
			return nil, where + " isn't in the tree"
		}
		return data, ""
	}
	var unreadable []assess.Patch
	for side, names := range [][]string{dropped, own} {
		for _, name := range names {
			data, why := read(side, name)
			if why != "" {
				unreadable = append(unreadable, assess.Patch{Result: patchcheck.Result{Name: name, Detail: why}, Dropped: side == 0})
				continue
			}
			patches = append(patches, patchcheck.Patch{Name: name, Data: data})
		}
	}
	results, err := patchcheck.Port(ctx, infos[1], paths, patches)
	if err != nil {
		return unchecked(err.Error())
	}
	for _, result := range results {
		found = append(found, assess.Patch{Result: result, Dropped: slices.Contains(dropped, result.Name)})
	}
	return append(found, unreadable...)
}

// readCommits reads a Git-fetched port's source at the commit each
// version's git.branch names, through its forge's archive of each commit,
// paired: the commit's files, as the forge archives them, submodules left
// out, never what a build's clone checked out, which its coverage says. The
// base's git.branch is resolved now, not when the base was made: a tag
// moved since names another commit, and the coverage says that too. A
// reading of a commit is kept by the commit, so an assessment made again
// asks the forge for nothing.
func (e *Engine) readCommits(ctx context.Context, infos [2]macports.PortInfo, hadBase bool) ([]assess.Pair, []model.Coverage, string, bool) {
	archiver, err := e.sourceArchiver()
	if err != nil {
		return nil, nil, err.Error(), false
	}
	directory, err := scratch.Dir("assess-")
	if err != nil {
		return nil, nil, err.Error(), false
	}
	defer os.RemoveAll(directory)
	readings := [2]project.Reading{{Layout: project.Enclosed, Files: map[string]project.File{}}}
	var commits [2]string
	for side, info := range infos {
		if side == 0 && !hadBase {
			continue
		}
		which := "the revision's"
		if side == 0 {
			which = "the base's"
		}
		declared, _, err := info.GitSource()
		if err != nil {
			return nil, nil, fmt.Sprintf("%s Git source couldn't be read: %v", which, err), false
		}
		resolved := e.resolveGit(ctx, declared)
		switch {
		case resolved.Unresolved != "":
			// Its refs couldn't be read, or a tag isn't there yet, which
			// another try may not meet, and reading refs again is cheap.
			return nil, nil, fmt.Sprintf("%s git.branch couldn't be resolved: %s", which, resolved.Unresolved), true
		case resolved.Commit == "":
			return nil, nil, fmt.Sprintf("%s git.branch is an abbreviated commit, %s, which only a clone expands", which, resolved.Abbreviation), false
		}
		commits[side] = string(resolved.Commit)
		spec := project.Spec{Subdirectory: macports.SourceSubdirectory(info.Options["worksrcdir"])}
		key := commitKey(commits[side])
		if reading, ok := e.readings().Kept(key, spec); ok {
			readings[side] = reading
			continue
		}
		path, err := archiver.SourceArchive(ctx, info, commits[side], directory)
		if err != nil {
			return nil, nil, fmt.Sprintf("%s commit %s couldn't be fetched from its forge: %v", which, commits[side], err), transient(err)
		}
		if readings[side], err = e.readings().Read(ctx, path, key, spec); err != nil {
			return nil, nil, fmt.Sprintf("reading %s commit %s: %v", which, commits[side], err), false
		}
	}
	reason := "read from the forge's archive of each commit, submodules left out"
	if hadBase {
		reason += "; the base's git.branch as it names a commit now"
	}
	coverage := []model.Coverage{{Path: commits[1], Relevance: "unknown", Treatment: "inspected", Reason: reason}}
	return []assess.Pair{{Archive: commits[1], Before: readings[0], After: readings[1]}}, coverage, "", false
}

// mirror is MacPorts' distfiles mirror, where an archive upstream no
// longer serves as the Portfile's checksums say is found: the one the
// engine was given, or MacPorts' own.
func (e *Engine) mirror() string {
	if e.ArchiveMirror != "" {
		return e.ArchiveMirror
	}
	return distfetch.MacPortsMirror
}

// transient reports a failure another try may not meet, as HTTP and the
// network say (fetch.Transient), and a forge's rate limit.
func transient(err error) bool {
	var limited *forge.RateLimitError
	return fetch.Transient(err) || errors.As(err, &limited)
}

// commitKey is what a reading of a commit is kept by: a digest of the
// commit, which is what a Git-fetched port's source observed is.
func commitKey(commit string) string {
	sum := sha256.Sum256([]byte("git commit " + commit))
	return hex.EncodeToString(sum[:])
}

// SourceArchiver writes a Git-fetched port's forge's archive of a commit,
// for its assessment.
type SourceArchiver interface {
	SourceArchive(ctx context.Context, port macports.PortInfo, commit, directory string) (string, error)
}

// sourceArchiver is the one the engine was given, or upstream discovery's
// forges.
func (e *Engine) sourceArchiver() (SourceArchiver, error) {
	if e.SourceArchiver != nil {
		return e.SourceArchiver, nil
	}
	ports, err := e.selectionReader()
	if err != nil {
		return nil, err
	}
	return e.discovery(ports), nil
}

// readPlans reads the archives each version's fetch plan names, paired by
// the port's source set (macports.MatchSources): one only the revision
// names is read beside nothing, as new, and one only the base names, or
// one several could correspond to, is returned unpaired, unread. A reading kept for an archive's content,
// by the sha256 its Portfile declares, stands without fetching it again;
// the rest are fetched as the Portfile's checksums declare them, and
// kept, in directory, the caller's, which the revision's fetched are
// returned by name from. What couldn't be fetched or read is the problem.
func (e *Engine) readPlans(ctx context.Context, infos [2]macports.PortInfo, plans [2][]macports.Distfile, hadBase bool, directory string) ([]assess.Pair, []macports.SourceMatch, map[string]string, string, bool) {
	type side struct {
		info     macports.PortInfo
		plan     []macports.Distfile
		declared map[string]portfile.Checksum
		spec     project.Spec
		readings map[string]project.Reading
	}
	var names [2][]string
	for i := range names {
		if i == 0 && !hadBase {
			continue
		}
		for _, file := range plans[i] {
			names[i] = append(names[i], file.Name)
		}
	}
	matches := macports.MatchSources(names[0], names[1], nil)
	// What's read is what the matched and added entries name.
	wanted := [2]map[string]bool{{}, {}}
	for _, match := range matches {
		if match.Status == macports.SourceMatched || match.Status == macports.SourceAdded {
			wanted[0][match.Before], wanted[1][match.After] = true, true
		}
	}
	var sides [2]side
	for i := range sides {
		if i == 0 && !hadBase {
			continue
		}
		sides[i] = side{info: infos[i], plan: plans[i], declared: distfetch.Declared(infos[i].Options["checksums"]),
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
	// The revision's archives fetched, by name, which the patch check
	// reads too.
	fetchedNow := map[string]string{}
	cache := e.readings()
	for i, s := range sides {
		var missing []macports.Distfile
		for _, file := range s.plan {
			if !wanted[i][file.Name] {
				continue
			}
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
			return nil, nil, nil, err.Error(), false
		}
		fetched, err := fetchPlanned(ctx, distfetch.Client{Mirror: e.mirror()}.Store(into), s.info, missing)
		if err != nil {
			which := "the revision's"
			if i == 0 {
				which = "the base's"
			}
			return nil, nil, nil, fmt.Sprintf("%s archives couldn't be fetched: %v", which, err), transient(err)
		}
		for _, archive := range fetched {
			reading, err := cache.Read(ctx, archive.Path, archive.Sum.SHA256, s.spec)
			if err != nil {
				return nil, nil, nil, fmt.Sprintf("reading %s: %v", archive.Name, err), false
			}
			s.readings[archive.Name] = reading
			if i == 1 {
				fetchedNow[archive.Name] = archive.Path
			}
		}
	}
	var pairs []assess.Pair
	var unpaired []macports.SourceMatch
	for _, match := range matches {
		switch match.Status {
		case macports.SourceMatched:
			pairs = append(pairs, assess.Pair{Archive: match.After, Before: sides[0].readings[match.Before], After: sides[1].readings[match.After], Match: match})
		case macports.SourceAdded:
			pairs = append(pairs, assess.Pair{Archive: match.After, Before: project.Reading{Layout: project.Enclosed, Files: map[string]project.File{}}, After: sides[1].readings[match.After], Match: match})
		default:
			unpaired = append(unpaired, match)
		}
	}
	return pairs, unpaired, fetchedNow, "", false
}
