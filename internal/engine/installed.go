package engine

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/evidence"
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
func (e *Engine) InstallChanges(ctx context.Context, run model.Run) ([]InstallFinding, error) {
	checked, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	var found []InstallFinding
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
	if err != nil || len(found) == 0 {
		return found, err
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

// installedLines are the install findings of the check a submission
// rests on as the description says them, naming the dependents the
// branch's revision bumps rebuild; none where it rests on none.
func (e *Engine) installedLines(ctx context.Context, checked *Evidence, edits []model.Edit) []string {
	if checked == nil || checked.Run.ID == "" {
		return nil
	}
	findings, err := e.InstallChanges(ctx, checked.Run)
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
