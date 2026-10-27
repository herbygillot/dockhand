package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/binaryarchive"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"
)

// InstallFinding is what a check's build installed against an earlier
// build of the port in the same environment (evidence.InstallChange), with
// the earlier build's check, and, where its own libraries' install names
// moved, the ports that link it directly, which --revbump-dependents
// rebuilds (Then's item 9: exiv2 0.28's soname and its 38 dependents).
type InstallFinding struct {
	evidence.InstallChange
	Check string
	// Version is the earlier build's version, where it's older than
	// master's, and Master master's (Then's items 9 and 10): a finding
	// against an old build may be the versions between's, not this one's.
	Version, Master string
	Dependents      []string
	// Unbumped is a change of files installed with neither the version
	// nor the revision raised, as the revision's Portfile has them against
	// its base's: installed copies keep the old files until rebuilt (the
	// revbump classifier's non-ABI half).
	Unbumped bool
	// earlierTree is the earlier build's revision's tree.
	earlierTree model.ObjectID
}

// InstallChanges compares each target a check built, in each environment,
// with the port's previous result there that dockhand checked, the newest
// passed build of another check: what moved of its own libraries, and the
// files gone or added. A target with no earlier reading says nothing,
// unknown and never equal. Each is a notice, holding nothing.
//
// fetch lets it fetch master's archive where none is kept yet; status,
// which reads no network itself, reads only an archive kept.
func (e *Engine) InstallChanges(ctx context.Context, run model.Run, fetch bool) ([]InstallFinding, error) {
	checked, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	var found []InstallFinding
	type pending struct {
		target      model.PlanTarget
		environment model.Environment
		result      model.TargetResult
	}
	var missing []pending
	err = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		for _, target := range checked.Targets {
			for _, cell := range target.Outcomes {
				result := cell.TargetResult
				if cell.Kind != evidence.CellRecorded || result.Outcome != model.OutcomePassed || result.ReusedFrom != "" || result.Installed == nil && result.Configure == nil {
					continue
				}
				if execution, ok := checked.Executions[result.Execution]; !ok || execution.Run != run.ID {
					continue
				}
				earlier, err := r.Reusable(target.Target.ID, cell.Environment, 20)
				if err != nil {
					return err
				}
				i := slices.IndexFunc(earlier, func(b store.Build) bool {
					return b.Execution.Run != run.ID && (b.Result.Installed != nil || b.Result.Configure != nil)
				})
				if i < 0 {
					// Never checked there: master's archive, read on the
					// host, is the earlier reading, its configure unknown.
					missing = append(missing, pending{target: target.Target, environment: cell.Environment, result: result})
					continue
				}
				change, _ := evidence.CompareInstalled(earlier[i].Result, result)
				// And what its configure found, against the same build
				// (Then's item 10).
				change.Found, change.Lost, _ = evidence.CompareConfigure(earlier[i].Result, result)
				if change.Empty() {
					continue
				}
				change.Target, change.Environment = target.Target.ID, cell.Environment
				finding := InstallFinding{InstallChange: change}
				if before, err := r.Run(earlier[i].Execution.Run); err == nil {
					finding.Check = before.Name()
					if revision, err := r.Revision(before.Revision); err == nil {
						finding.earlierTree = revision.Source.Tree
					}
				}
				found = append(found, finding)
			}
		}
		return nil
	})
	if err != nil {
		return found, err
	}
	for _, p := range missing {
		earlier, name, ok := e.masterArchive(ctx, p.target, p.environment, fetch)
		if !ok {
			continue
		}
		change, ok := evidence.CompareInstalled(earlier, p.result)
		if !ok || change.Empty() {
			continue
		}
		change.Target, change.Environment = p.target.ID, p.environment
		found = append(found, InstallFinding{InstallChange: change, Check: "master's archive " + name})
	}
	if len(found) == 0 {
		return found, nil
	}
	// The ports that link one whose libraries moved, as the port index at
	// the check's revision has them.
	revision, err := e.Revision(ctx, run.Revision)
	if err != nil {
		return found, nil
	}
	trees, _ := e.Repo.CommitTrees(ctx, []string{string(revision.Source.Base)})
	master, hasMaster := e.lastMaster(ctx)
	var masterTree string
	if hasMaster {
		if masters, err := e.Repo.CommitTrees(ctx, []string{string(master)}); err == nil {
			masterTree = masters[string(master)]
		}
	}
	order := e.versionOrder(ctx)
	for i := range found {
		directory := ""
		for _, target := range checked.Plan.Targets {
			if target.ID == found[i].Target {
				directory = target.Directory
			}
		}
		// The earlier build's version, said where it's older than master's.
		if found[i].earlierTree != "" && masterTree != "" && order != nil {
			_, then, errThen := e.Repo.File(ctx, string(found[i].earlierTree), directory+"/Portfile")
			_, now, errNow := e.Repo.File(ctx, masterTree, directory+"/Portfile")
			if errThen == nil && errNow == nil {
				was, knownWas := portfile.DeclaredVersion(then, "")
				is, knownIs := portfile.DeclaredVersion(now, "")
				if lower, ok := order(was, is); knownWas && knownIs && ok && lower < 0 {
					found[i].Version, found[i].Master = was, is
				}
			}
		}
		if len(found[i].Added)+len(found[i].Removed) > 0 && trees != nil {
			_, before, errBefore := e.Repo.File(ctx, trees[string(revision.Source.Base)], directory+"/Portfile")
			_, after, errAfter := e.Repo.File(ctx, string(revision.Source.Tree), directory+"/Portfile")
			if errBefore == nil && errAfter == nil {
				versionBefore, known := portfile.DeclaredVersion(before, "")
				versionAfter, knownAfter := portfile.DeclaredVersion(after, "")
				revisionBefore, _, _ := portfile.DeclaredRevision(before)
				revisionAfter, _, _ := portfile.DeclaredRevision(after)
				found[i].Unbumped = known && knownAfter && versionBefore == versionAfter && revisionBefore == revisionAfter
			}
		}
		if len(found[i].Libraries) == 0 {
			continue
		}
		dependents, err := e.dependents(ctx, revision.Source, []string{directory})
		if err != nil {
			continue
		}
		for _, dependent := range dependents {
			if slices.Contains(dependent.Phases, "library") && !slices.Contains(found[i].Dependents, dependent.Name) {
				found[i].Dependents = append(found[i].Dependents, dependent.Name)
			}
		}
		slices.Sort(found[i].Dependents)
	}
	return found, nil
}

// masterArchive is the earlier reading of a target never checked in an
// environment: the archive packages.macports.org serves for master's
// version, revision, and default variants on that release and arch, read
// on the host (binaryarchive.Contents), its files under the
// environment's prefix. It's fetched once and kept by its name, an
// archive not served marked so, for a license MacPorts doesn't
// distribute or a release its buildbots don't build; false there, which
// says nothing.
func (e *Engine) masterArchive(ctx context.Context, target model.PlanTarget, environment model.Environment, fetch bool) (model.TargetResult, string, bool) {
	darwin, err := strconv.Atoi(environment.Platform.Version)
	arch := environment.Platform.Architecture
	master, ok := e.lastMaster(ctx)
	if err != nil || arch == "" || environment.Platform.OS != "darwin" || !ok {
		return model.TargetResult{}, "", false
	}
	reader, err := e.portReader()
	if err != nil {
		return model.TargetResult{}, "", false
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(master)})
	if err != nil {
		return model.TargetResult{}, "", false
	}
	infos, err := reader.Ports(ctx, model.Source{Commit: master, Base: master, Tree: model.ObjectID(trees[string(master)])}, target.Directory, environment, nil)
	if err != nil {
		return model.TargetResult{}, "", false
	}
	i := slices.IndexFunc(infos, func(info macports.PortInfo) bool { return info.Name == target.Target.Name })
	if i < 0 {
		return model.TargetResult{}, "", false
	}
	info := infos[i]
	var defaults []string
	if variants, err := info.Variants(); err == nil {
		for _, variant := range variants {
			if variant.Default {
				defaults = append(defaults, variant.Name)
			}
		}
	}
	name := binaryarchive.ArchiveName(info.Name, info.Version, strconv.Itoa(info.Revision), defaults, darwin, arch)
	if !binaryarchive.Installable(info.Name, name) {
		return model.TargetResult{}, "", false
	}
	path := filepath.Join(e.ArchiveDirectory(), "master", name)
	if _, err := os.Stat(path + ".none"); err == nil {
		return model.TargetResult{}, "", false
	}
	if _, err := os.Stat(path); err != nil {
		if !fetch || !e.fetchArchive(ctx, binaryarchive.URL(info.Name, name), path) {
			return model.TargetResult{}, "", false
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return model.TargetResult{}, "", false
	}
	defer file.Close()
	files, libraries, err := binaryarchive.Contents(file)
	if err != nil {
		return model.TargetResult{}, "", false
	}
	// The archive's prefix is the buildbots', /opt/local; a check in a
	// prefix of the person's has its own.
	if prefix := environment.Prefix; prefix != "" && prefix != macports.DefaultPrefix {
		moved := func(p string) string {
			if rest, ok := strings.CutPrefix(p, macports.DefaultPrefix+"/"); ok {
				return prefix + "/" + rest
			}
			return p
		}
		for i := range files {
			files[i] = moved(files[i])
		}
		for i := range libraries {
			libraries[i].File, libraries[i].Name = moved(libraries[i].File), moved(libraries[i].Name)
		}
	}
	return model.TargetResult{Target: target.ID, Installed: files, Libraries: libraries}, name, true
}

// fetchArchive fetches an archive to path, whole or not at all; one the
// site doesn't serve is marked, path.none, so it isn't asked for again.
func (e *Engine) fetchArchive(ctx context.Context, url, path string) bool {
	if base := e.options.Packages; base != "" {
		url = base + strings.TrimPrefix(url, binaryarchive.Packages)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	response, err := fetch.Client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		_ = os.WriteFile(path+".none", nil, 0o644)
		return false
	}
	if response.StatusCode != http.StatusOK {
		return false
	}
	partial := path + ".part"
	out, err := os.Create(partial)
	if err != nil {
		return false
	}
	if _, err := io.Copy(out, response.Body); err != nil || out.Close() != nil {
		_ = os.Remove(partial)
		return false
	}
	return os.Rename(partial, path) == nil
}

// installedLines are the install findings of the check a submission
// rests on as the description says them, naming the dependents the
// branch's revision bumps rebuild; none where it rests on none.
func (e *Engine) installedLines(ctx context.Context, checked *Evidence, edits []model.Edit) []string {
	if checked == nil || checked.Run.ID == "" {
		return nil
	}
	findings, err := e.InstallChanges(ctx, checked.Run, true)
	if err != nil {
		return nil
	}
	var bumped []string
	for _, edit := range edits {
		if edit.Kind == model.EditRevbump {
			bumped = append(bumped, edit.Port)
		}
	}
	var lines []string
	for _, finding := range findings {
		for _, line := range finding.Lines(bumped) {
			lines = append(lines, line.Words)
		}
	}
	return lines
}

// InstallLine is one line of what a finding says, and whether it asks a
// person's look.
type InstallLine struct {
	Look  bool
	Words string
}

// Lines say a finding as the check's report, status, and the pull
// request's description say it: its own libraries whose install names
// moved, with the ports that link it, those of them bumped named where
// the branch bumps any, and what configure no longer or now finds, each
// asking a look; and the files gone or added, asking one where neither
// the version nor the revision was raised.
func (f InstallFinding) Lines(bumped []string) []InstallLine {
	var lines []InstallLine
	against := ""
	if f.Check != "" {
		against = " against " + f.Check
	}
	if f.Version != "" {
		against += fmt.Sprintf(" (%s, older than master's %s)", f.Version, f.Master)
	}
	where := DescribeEnvironment(f.Environment)
	for _, library := range f.Libraries {
		words := fmt.Sprintf("%s: %s on %s%s", f.Target, library.Words(), where, against)
		switch {
		case len(f.Dependents) == 0:
			words += "; no port links it"
		default:
			words += fmt.Sprintf("; %s link it", prose.Few(f.Dependents, 6, "port"))
			var rebuilt []string
			for _, dependent := range f.Dependents {
				if slices.Contains(bumped, dependent) {
					rebuilt = append(rebuilt, dependent)
				}
			}
			switch {
			case len(rebuilt) == len(f.Dependents):
				words += ", and this branch rebuilds each"
			case len(rebuilt) > 0:
				words += fmt.Sprintf(", and this branch rebuilds %s", prose.Few(rebuilt, 6, "port"))
			default:
				words += ", which update --revbump-dependents rebuilds"
			}
		}
		lines = append(lines, InstallLine{true, words})
	}
	if len(f.Found)+len(f.Lost) > 0 {
		var said []string
		if len(f.Lost) > 0 {
			said = append(said, "configure no longer says "+prose.FewJoined(quoted(f.Lost), 3, "line", ", "))
		}
		if len(f.Found) > 0 {
			said = append(said, "now says "+prose.FewJoined(quoted(f.Found), 3, "line", ", "))
		}
		lines = append(lines, InstallLine{true, fmt.Sprintf("%s: %s on %s%s", f.Target, strings.Join(said, "; "), where, against)})
	}
	var files []string
	if len(f.Removed) > 0 {
		files = append(files, "no longer installs "+prose.Few(f.Removed, 4, "file"))
	}
	if len(f.Added) > 0 {
		files = append(files, "now installs "+prose.Few(f.Added, 4, "file"))
	}
	switch {
	case len(files) > 0 && f.Unbumped:
		lines = append(lines, InstallLine{true, fmt.Sprintf("%s: %s on %s%s, with neither the version nor the revision raised, so installed copies keep the old files; raise the revision", f.Target, strings.Join(files, "; "), where, against)})
	case len(files) > 0:
		lines = append(lines, InstallLine{false, fmt.Sprintf("%s: %s on %s%s", f.Target, strings.Join(files, "; "), where, against)})
	}
	return lines
}

// quoted are lines quoted, as a sentence names what a log said.
func quoted(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = strconv.Quote(line)
	}
	return out
}
