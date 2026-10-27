package comparison

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	archivefile "github.com/herbygillot/dockhand/internal/archive"
	"github.com/herbygillot/dockhand/internal/editprep"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/sourcecompare"
)

// binaryPackage starts what reading a revision's archives says of one
// that's a binary package, which the assessment says as nothing to
// compare.
const binaryPackage = "binary package: "

// Revision assesses one port of a directory at the revision against the
// base, as this Mac's context fetches it: names are the port in the base,
// its own or a sibling's (macports.CounterpartIn), and in the revision. A port new to the directory has
// no base, and its candidate is assessed alone, with nothing said of a
// missing old archive; a port that fetches no upstream source has nothing
// to compare, and says so.
func (c Collector) Revision(ctx context.Context, sources [2]model.Source, directory string, names [2]string, hadBase bool, portfile []byte) model.UpstreamComparison {
	name := names[1]
	var infos [2]macports.PortInfo
	var plans [2][]macports.Distfile
	problem := ""
	// kind is the problem's kind, which says whether another try may meet
	// it.
	var kind refusal.Kind
	fetches := true
	for side, source := range sources {
		if side == 0 && !hadBase {
			continue
		}
		info, plan, err := c.Planner.ArchivePlan(ctx, source, directory, names[side])
		switch {
		case errors.Is(err, treequery.ErrNoArchives):
			if side == 1 {
				fetches = false
			}
		case errors.Is(err, treequery.ErrNoPort) && side == 0:
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
	case hadBase && macports.SameSource(infos[0], infos[1]):
		coverage = append(coverage, model.Coverage{Path: directory, Relevance: "unknown", Treatment: "inspected", Policy: PolicySourceUnchanged,
			Reason: name + "'s source is the base's, so there's nothing upstream to compare"})
	case infos[1].GitFetched():
		input.Pairs, coverage, problem, kind = c.readCommits(ctx, infos, hadBase)
	case !fetches:
		reason := name + " fetches no upstream source, so there's nothing to compare"
		// A python stub over its subports fetches nothing itself, and
		// its subports' source is compared: said so, rather than read as
		// a port with no source (the Vx port's field testing, 2026-10-04,
		// py-coremltools and py-pyaml).
		if only, err := infos[1].MetadataOnly(); err == nil && only {
			reason = name + " builds nothing itself, so its subports' source is what's compared"
		}
		coverage = append(coverage, model.Coverage{Path: directory, Relevance: "unknown", Treatment: "inspected", Policy: PolicyNotCompared, Reason: reason})
	default:
		scratchDirectory, err := scratch.Dir("assess-")
		if err != nil {
			problem = err.Error()
			break
		}
		defer os.RemoveAll(scratchDirectory)
		var fetched map[string]string
		input.Pairs, input.Unpaired, fetched, problem, kind = c.readPlans(ctx, infos, plans, hadBase, scratchDirectory)
		// A port that fetches a binary, as 1password-cli's installer
		// package, has no source to compare, which is said and holds
		// nothing, as for a port that fetches none (field testing's batch
		// 10, finding 2).
		if words, ok := strings.CutPrefix(problem, binaryPackage); ok {
			coverage = append(coverage, model.Coverage{Path: directory, Relevance: "unknown", Treatment: "inspected", Policy: PolicyNotCompared, Reason: name + " ships " + words})
			problem = ""
			break
		}
		if problem == "" {
			input.Patches = c.patchesFor(ctx, patchRequest{infos: infos, sources: sources, portdir: directory, plan: plans[1], fetched: fetched, scratch: scratchDirectory})
		}
	}
	comparison := c.collect(ctx, input, sources, portfile, problem == "")
	comparison.Problem = problem
	if problem != "" {
		comparison.ProblemKind = string(kind)
	}
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
// architecture review's finding 1): the candidate's Portfile as its caller read it, the providers its requirements name observed where its archives
// were read, and the rules applied.
func (c Collector) collect(ctx context.Context, input assess.Input, trees [2]model.Source, portfile []byte, observe bool) model.UpstreamComparison {
	input.Portfile = portfile
	if observe && len(input.Pairs) > 0 {
		input.Observed = c.observe(ctx, input, trees)
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
func (c Collector) patchesFor(ctx context.Context, request patchRequest) []assess.Patch {
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
			if infos[1].Extracts(name) {
				paths = append(paths, request.fetched[name])
			}
		}
	}
	for _, file := range request.plan {
		if !infos[1].Extracts(file.Name) {
			// MacPorts patches what it extracted, which this isn't.
			continue
		}
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
		archives, err := treequery.FetchPlanned(ctx, distfetch.Client{Mirror: c.mirror()}.Store(into), infos[1], missed)
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
		data, exists, err := c.Files.File(ctx, sources[side].Tree, where)
		switch {
		case err != nil:
			return nil, err.Error()
		case !exists:
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
	results, err := patchcheck.Port(ctx, infos[1], paths, patches, false)
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
func (c Collector) readCommits(ctx context.Context, infos [2]macports.PortInfo, hadBase bool) ([]assess.Pair, []model.Coverage, string, refusal.Kind) {
	archiver, err := c.archiver()
	if err != nil {
		return nil, nil, err.Error(), ""
	}
	directory, err := scratch.Dir("assess-")
	if err != nil {
		return nil, nil, err.Error(), ""
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
			return nil, nil, fmt.Sprintf("%s Git source couldn't be read: %v", which, err), ""
		}
		resolved := c.Resolver.Resolve(ctx, declared)
		switch {
		case resolved.Missing:
			// The repository has no such ref yet, as a tag not yet pushed:
			// looked at again a day later, as a refused archive is.
			return nil, nil, fmt.Sprintf("%s git.branch couldn't be resolved: %s", which, resolved.Unresolved), refusal.UpstreamMissing
		case resolved.Unresolved != "":
			// Its refs couldn't be read, a clone failing or timing out,
			// which another try may meet at once: it was Failed, which
			// lookAgain never made again, against its own comment (C8-5).
			return nil, nil, fmt.Sprintf("%s git.branch couldn't be resolved: %s", which, resolved.Unresolved), refusal.Network
		case resolved.Commit == "":
			return nil, nil, fmt.Sprintf("%s git.branch is an abbreviated commit, %s, which only a clone expands", which, resolved.Abbreviation), ""
		}
		commits[side] = string(resolved.Commit)
		spec := info.ReadingSpec()
		key := commitKey(commits[side])
		if reading, ok := c.Readings.Kept(key, spec); ok {
			readings[side] = reading
			continue
		}
		path, err := archiver.SourceArchive(ctx, info, resolved, directory)
		if err != nil {
			return nil, nil, fmt.Sprintf("%s commit %s couldn't be fetched from its forge: %v", which, commits[side], err), refusal.KindOf(err)
		}
		if readings[side], err = c.Readings.Read(ctx, path, key, spec); err != nil {
			return nil, nil, fmt.Sprintf("reading %s commit %s: %v", which, commits[side], err), ""
		}
	}
	reason := "read from the forge's archive of each commit, submodules left out"
	if hadBase {
		reason += "; the base's git.branch as it names a commit now"
	}
	coverage := []model.Coverage{{Path: commits[1], Relevance: "unknown", Treatment: "inspected", Reason: reason}}
	return []assess.Pair{{Archive: commits[1], Before: readings[0], After: readings[1]}}, coverage, "", ""
}

// mirror is MacPorts' distfiles mirror, where an archive upstream no
// longer serves as the Portfile's checksums say is found: the one the
// collector was given, or MacPorts' own.
func (c Collector) mirror() string {
	if c.Mirror != "" {
		return c.Mirror
	}
	return distfetch.MacPortsMirror
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
	SourceArchive(ctx context.Context, port macports.PortInfo, source model.GitSource, directory string) (string, error)
}

// readDistfile reads a distfile as MacPorts treats it: one it extracts as
// the archive of a project, kept by its content's digest (project.Cache),
// and one it doesn't, as bashunit's script, as the file it is
// (project.ReadFile), which an archive's reader refused as no tar (field
// testing at 285a496f).
func (c Collector) readDistfile(ctx context.Context, info macports.PortInfo, name, filename, digest string, spec project.Spec) (project.Reading, error) {
	if !info.Extracts(name) {
		return project.ReadFile(filename, name)
	}
	return c.Readings.Read(ctx, filename, digest, spec)
}

// readPlans reads the archives each version's fetch plan names, paired by
// the port's source set (macports.MatchSources): one only the revision
// names is read beside nothing, as new, and one only the base names, or
// one several could correspond to, is returned unpaired, unread. A reading kept for an archive's content,
// by the sha256 its Portfile declares, stands without fetching it again;
// the rest are fetched as the Portfile's checksums declare them, and
// kept, in directory, the caller's, which the revision's fetched are
// returned by name from. What couldn't be fetched or read is the problem.
func (c Collector) readPlans(ctx context.Context, infos [2]macports.PortInfo, plans [2][]macports.Distfile, hadBase bool, directory string) ([]assess.Pair, []macports.SourceMatch, map[string]string, string, refusal.Kind) {
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
			spec: infos[i].ReadingSpec(), readings: map[string]project.Reading{}}
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
	cache := c.Readings
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
			return nil, nil, nil, err.Error(), ""
		}
		fetched, err := treequery.FetchPlanned(ctx, distfetch.Client{Mirror: c.mirror()}.Store(into), s.info, missing)
		if err != nil {
			which := "the revision's"
			if i == 0 {
				which = "the base's"
			}
			return nil, nil, nil, fmt.Sprintf("%s archives couldn't be fetched: %v", which, err), refusal.KindOf(err)
		}
		for _, archive := range fetched {
			reading, err := c.readDistfile(ctx, s.info, archive.Name, archive.Path, archive.Sum.SHA256, s.spec)
			if errors.Is(err, archivefile.ErrBinary) {
				return nil, nil, nil, binaryPackage + archive.Name + ", which is a binary package, with no source to compare", ""
			}
			if err != nil {
				return nil, nil, nil, fmt.Sprintf("reading %s: %v", archive.Name, err), ""
			}
			s.readings[archive.Name] = reading
			if i == 1 && s.info.Extracts(archive.Name) {
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
	return pairs, unpaired, fetchedNow, "", ""
}

// PolicyNotCompared is the coverage policy of a port that fetches no upstream
// source, which has nothing to compare; PolicySourceUnchanged of one whose
// source a change left as the base had it, a subport beside the one
// changed.
const (
	PolicyNotCompared     = "not-compared"
	PolicySourceUnchanged = "source-unchanged"
)

// Update assesses what upstream's change means for the port an update
// prepared (the assessment design, C): each archive the update replaced
// beside the one that replaces it, in every context that fetches them, as
// gh's source tarball and the prebuilt zip older macOS fetches each have
// their own, read where the port builds there; what the new version's
// Python requirements ask of the ports that provide them, observed in the
// tree before the edit and after; and what its go.mod asks of
// go.toolchain_min, whether or not archives are compared (compare).
// portfile is the candidate's Portfile. Not being able to compare is its
// Problem, never a reason to refuse the update.
func (c Collector) Update(ctx context.Context, result editprep.Result, versions sourcecompare.Versions, trees [2]model.Source, compare bool, portfile []byte) model.UpstreamComparison {
	input := assess.Input{Versions: versions}
	input.Base, _ = result.PortBefore(result.Target.Name)
	input.Port, _ = result.PortAfter(result.Target.Name)
	if t := result.GoToolchain; t != nil {
		input.Toolchain = &assess.Toolchain{Required: t.Required, Declared: t.Declared, Outcome: toolchainOutcomes[t.Outcome], Problem: t.Problem}
	}
	problem := ""
	// read is how the source was read where it wasn't from the update's
	// archives: a Git-fetched port's commits through its forge, as a
	// revision's assessment reads them, or nothing, for a port that
	// fetches nothing. Either is said, never left silent: semgrep's
	// update compared nothing and said nothing (field testing, 2026-10-02).
	var read []model.Coverage
	var kind refusal.Kind
	viaGit := false
	switch {
	case !compare:
	case result.PreviousProblem != "":
		// A mirror's outage or a rate limit is transient, as the revision's
		// assessment has it, so a later look fetches them again rather
		// than hold on a stale reason until a file is edited (the
		// architecture review's L2b).
		problem = "the current version's archives could not be fetched: " + result.PreviousProblem
		kind = result.PreviousKind
	case len(result.Downloads) == 0 && input.Port.GitFetched():
		viaGit = true
		input.Pairs, read, problem, kind = c.readCommits(ctx, [2]macports.PortInfo{input.Base, input.Port}, true)
	case len(result.Downloads) == 0:
		read = []model.Coverage{{Path: path.Dir(result.Target.Portfile), Relevance: "unknown", Treatment: "inspected", Policy: PolicyNotCompared, Reason: result.Target.Name + " fetches no upstream source, so there's nothing to compare"}}
	default:
		for _, download := range result.Downloads {
			if !slices.ContainsFunc(result.Pairs, func(pair editprep.ArchivePair) bool { return pair.Next.Name == download.Name }) {
				problem = download.Name + " replaces no archive dockhand could find, so it wasn't compared"
				break
			}
		}
	}
	if compare && problem == "" && len(result.Downloads) > 0 {
		pairs, err := c.readPairs(ctx, result.Pairs, input.Base, input.Port)
		switch {
		case errors.Is(err, archivefile.ErrBinary):
			// A binary package holds no source: said, holding nothing,
			// as for a port that fetches none (field testing's batch 10,
			// finding 2: 1password-cli).
			read = append(read, model.Coverage{Path: path.Dir(result.Target.Portfile), Relevance: "unknown", Treatment: "inspected", Policy: PolicyNotCompared,
				Reason: binaryReason(result.Target.Name, err)})
			pairs = nil
		case err != nil:
			problem = err.Error()
		}
		input.Pairs = pairs
	}
	if compare && problem == "" && len(result.Downloads) > 0 {
		// Its patches are checked as a revision's are, the preparation's
		// own results taken as they stand, against the archives it kept:
		// update's assessment had none, and stood as the revision's whole
		// one (the architecture review's finding 1).
		fetched := map[string]string{}
		for _, download := range result.Downloads {
			if download.Path != "" {
				fetched[download.Name] = download.Path
			}
		}
		input.Patches = c.patchesFor(ctx, patchRequest{infos: [2]macports.PortInfo{input.Base, input.Port}, sources: trees,
			portdir: path.Dir(result.Target.Portfile), fetched: fetched, checked: result.Patches})
	}
	comparison := c.collect(ctx, input, trees, portfile, problem == "")
	comparison.Problem = problem
	if problem != "" {
		comparison.ProblemKind = string(kind)
	}
	if viaGit && problem == "" && len(input.Pairs) == 1 {
		comparison.Commit = input.Pairs[0].Archive
	}
	comparison.Coverage = append(comparison.Coverage, read...)
	return comparison
}

// toolchainOutcomes are what the editor did about go.toolchain_min, as the
// assessment words them.
var toolchainOutcomes = map[editprep.GoToolchainOutcome]string{
	editprep.GoToolchainCovered: assess.ToolchainCovered, editprep.GoToolchainRaised: assess.ToolchainRaised,
	editprep.GoToolchainUndeclared: assess.ToolchainUndeclared, editprep.GoToolchainByHand: assess.ToolchainByHand,
	editprep.GoToolchainUnchecked: assess.ToolchainUnchecked,
}

// readPairs reads each pair of archives, each version where its port
// builds in the context that fetches it (the assessment design's step 1):
// a monorepo's Python bindings in bindings/python. A pair a context didn't
// name the port for is read with the update's own. What's read is kept,
// by the archive's content, for an assessment made again. Each pair is the
// editor's observed replacement, its entry in the port's source set as a
// revision's assessment's are (macports.MatchSources).
func (c Collector) readPairs(ctx context.Context, pairs []editprep.ArchivePair, base, port macports.PortInfo) ([]assess.Pair, error) {
	var before, after []string
	var observed [][2]string
	for _, pair := range pairs {
		before, after = append(before, pair.Previous.Name), append(after, pair.Next.Name)
		observed = append(observed, [2]string{pair.Previous.Name, pair.Next.Name})
	}
	matches := macports.MatchSources(before, after, observed)
	var read []assess.Pair
	for _, pair := range pairs {
		if pair.Previous.Path == "" || pair.Next.Path == "" {
			return nil, errors.New("the archives were not kept to compare")
		}
		ports := [2]macports.PortInfo{pair.Base, pair.Port}
		for i, fallback := range []macports.PortInfo{base, port} {
			if ports[i].Name == "" {
				ports[i] = fallback
			}
		}
		var readings [2]project.Reading
		for i, archive := range []distfetch.Download{pair.Previous, pair.Next} {
			reading, err := c.readDistfile(ctx, ports[i], archive.Name, archive.Path, archive.SHA256, ports[i].ReadingSpec())
			// Named for the distfile, not the temporary file it was
			// fetched to: "ships source-906474379 is a macOS installer
			// package" (field testing at 5eb101a3, 1password-cli).
			if binary := new(archivefile.BinaryError); errors.As(err, &binary) {
				binary.File = archive.Name
				return nil, binary
			}
			if err != nil {
				return nil, fmt.Errorf("reading %s: %v", path.Base(archive.Path), err)
			}
			readings[i] = reading
		}
		var match macports.SourceMatch
		if i := slices.IndexFunc(matches, func(m macports.SourceMatch) bool { return m.After == pair.Next.Name }); i >= 0 {
			match = matches[i]
		}
		read = append(read, assess.Pair{Archive: pair.Next.Name, Before: readings[0], After: readings[1], Port: pair.Port, Match: match})
	}
	return read, nil
}

// observe observes what an assessment wants, until it wants nothing
// more: the version of each port that provides a Python requirement in
// question, in the tree before the edit or after, as the Observer finds
// it. What can't be observed is said as such, which the assessment weighs
// as not knowing.
func (c Collector) observe(ctx context.Context, input assess.Input, trees [2]model.Source) map[assess.Provider]assess.Observation {
	observed := map[assess.Provider]assess.Observation{}
	input.Observed = observed
	for {
		wanted := assess.Wanted(input)
		if len(wanted) == 0 {
			return observed
		}
		for _, provider := range wanted {
			if c.Observer == nil {
				observed[provider] = assess.Observation{Problem: "couldn't read the ports"}
				continue
			}
			tree := trees[1]
			if provider.Base {
				tree = trees[0]
			}
			observed[provider] = c.Observer.Observe(ctx, tree, provider.Port)
		}
	}
}

// binaryReason says a port ships a binary, not source: "1password-cli
// ships a macOS installer package (op_apple_universal_v2.31.0.pkg), with
// no source to compare".
func binaryReason(port string, err error) string {
	if binary := new(archivefile.BinaryError); errors.As(err, &binary) {
		return fmt.Sprintf("%s ships %s (%s), with no source to compare", port, binary.Kind, binary.File)
	}
	return port + " ships a binary package, with no source to compare"
}
