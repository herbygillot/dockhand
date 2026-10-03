package engine

import (
	"context"
	"path"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// FamilyReader evaluates a port directory in this Mac's own context, every
// subport of it, with the root it was evaluated in, as a change record
// compares it. MacPorts' evaluator is one; a PortReader that isn't makes
// no records, and its revisions keep their directories' text scope.
type FamilyReader interface {
	Family(ctx context.Context, source model.Source, directory string) (macports.Snapshot, error)
}

// revisionChanges are what a revision changed in each port directory its
// files change, against the base it was captured on, by directory: those
// recorded under this policy, and, where collect, those made now for the
// rest, which are recorded. A directory with no record keeps its text
// scope: every subport of it reads as changed, as before records. An
// adopted pull request's branch gets none, since its Portfile isn't the
// person's (the trust rule), and neither does a revision no evaluator
// can read.
func (e *Engine) revisionChanges(ctx context.Context, id model.BranchID, base, tree model.ObjectID, collect bool) (map[string]model.ChangeRecord, error) {
	records := map[string]model.ChangeRecord{}
	adopted := false
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		branch, err := r.Branch(id)
		if err != nil {
			return err
		}
		if adopted = branch.PullRequest != nil && branch.PullRequest.Adopted; adopted {
			return nil
		}
		recorded, err := r.ChangeRecords(store.AssessmentFilter{Branch: id, Tree: tree, Base: base})
		for _, record := range recorded {
			if _, ok := records[record.Directory]; !ok && record.Policy == fidelity.ChangePolicy {
				records[record.Directory] = record
			}
		}
		return err
	}); err != nil {
		return nil, err
	}
	if adopted || !collect {
		return records, nil
	}
	reader, err := e.portReader()
	if err != nil {
		return nil, err
	}
	family, ok := reader.(FamilyReader)
	if !ok {
		return records, nil
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(base)})
	if err != nil {
		return nil, err
	}
	baseTree := model.ObjectID(trees[string(base)])
	changed, err := e.Repo.ChangedPaths(ctx, string(baseTree), string(tree))
	if err != nil {
		return nil, err
	}
	sources := [2]model.Source{{Commit: base, Tree: baseTree, Base: base}, {Tree: tree, Base: base}}
	var made []model.ChangeRecord
	for _, directory := range macports.ScopeOf(changed).Ports {
		if _, ok := records[directory]; ok {
			continue
		}
		record := model.ChangeRecord{Branch: id, Tree: tree, Base: base, Directory: directory, Policy: fidelity.ChangePolicy, At: e.now()}
		var sides [2]*macports.Snapshot
		for i, source := range sources {
			file, _, err := e.Repo.File(ctx, string(source.Tree), directory+"/Portfile")
			if err != nil {
				return nil, err
			}
			if !file.Exists {
				continue
			}
			snapshot, err := family.Family(ctx, source, directory)
			if err != nil {
				record.Problem = sideWords[i] + ": " + err.Error()
				break
			}
			sides[i] = &snapshot
			record.Platform = snapshot.Platform
		}
		if record.Problem == "" {
			record.Ports = fidelity.SubportChanges(directory, sides[0], sides[1])
		}
		made = append(made, record)
		records[directory] = record
	}
	if len(made) == 0 {
		return records, nil
	}
	return records, e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		for _, record := range made {
			if err := tx.RecordChange(record); err != nil {
				return err
			}
		}
		return nil
	})
}

// sideWords say which side of a change record couldn't be evaluated.
var sideWords = [2]string{"the base couldn't be evaluated", "the revision couldn't be evaluated"}

// changedSubports are the subports of a directory a revision changes, as
// its record says, and whether it says: a directory with no record, or
// one whose record couldn't be made, has its text scope, every subport.
// So does one whose record finds no subport changed though its files
// did: the change is in what this Mac's evaluation doesn't see, such as
// a block for another macOS release (the multi-subport sweep: openssh's
// for macOS 27, py-tkinter's for older ones).
func changedSubports(records map[string]model.ChangeRecord, directory string) ([]string, bool) {
	record, ok := records[directory]
	if !ok || record.Problem != "" {
		return nil, false
	}
	changed := record.Changed()
	return changed, len(changed) > 0
}

// recordedChange says whether a subport of a directory is one the
// revision changes: by its record where there is one, else as the
// directory's text says, yes.
func recordedChange(records map[string]model.ChangeRecord, directory, port string) bool {
	changed, ok := changedSubports(records, directory)
	return !ok || slices.Contains(changed, port)
}

// recordedPorts are the ports changed directories change, as their change
// records say: each directory's changed subports, and else the port the
// directory is named for, its text saying only that some of its subports
// may have changed, with a note of why: its record isn't made yet, or
// found nothing this Mac's evaluation sees.
func recordedPorts(directories []string, records map[string]model.ChangeRecord) (ports []string, notes map[string]string) {
	notes = map[string]string{}
	for _, directory := range directories {
		if changed, ok := changedSubports(records, directory); ok {
			ports = append(ports, changed...)
			continue
		}
		port := path.Base(directory)
		ports = append(ports, port)
		notes[port] = "not yet evaluated"
		if record, ok := records[directory]; ok && record.Problem == "" {
			notes[port] = "no change this Mac's evaluation sees"
		}
	}
	return ports, notes
}
