package engine

import (
	"context"
	"slices"

	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// InstallFinding is what a check's build installed against an earlier
// build of the port in the same environment (evidence.InstallChange), with
// the earlier build's check, and, where its own libraries' install names
// moved, the ports that link it directly, which --revbump-dependents
// rebuilds (Then's item 9: exiv2 0.28's soname and its 38 dependents).
type InstallFinding struct {
	evidence.InstallChange
	Check      string
	Dependents []string
	// Unbumped is a change of files installed with neither the version
	// nor the revision raised, as the revision's Portfile has them against
	// its base's: installed copies keep the old files until rebuilt (the
	// revbump classifier's non-ABI half).
	Unbumped bool
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
	for i := range found {
		directory := ""
		for _, target := range checked.Plan.Targets {
			if target.ID == found[i].Target {
				directory = target.Directory
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
