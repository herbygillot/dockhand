package engine

import (
	"context"
	"path"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
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
// rest, and again for any whose record couldn't be made, which are
// recorded. Without collect, nothing is evaluated: a record that couldn't
// be made is read as it is, saying why. A directory with no record keeps its text
// scope: every subport of it reads as changed, as before records. An
// adopted pull request's branch gets them where the trust rule lets the
// work evaluate its author's Portfiles (mayEvaluate), and a revision no
// evaluator can read gets none.
func (e *Engine) revisionChanges(ctx context.Context, id model.BranchID, base, tree model.ObjectID, collect bool) (map[string]model.ChangeRecord, error) {
	records := map[string]model.ChangeRecord{}
	var branch model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if branch, err = r.Branch(id); err != nil {
			return err
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
	if !collect {
		return records, nil
	}
	ports, err := e.capabilities()
	if err != nil {
		return nil, err
	}
	family := ports.Families
	if family == nil {
		progress.VerboseReport(ctx, "no change records are made: nothing here evaluates a port's family")
		return records, nil
	}
	analysis, err := e.analyze(ctx, base, tree)
	if err != nil {
		return nil, err
	}
	sources := analysis.Sources()
	var made []model.ChangeRecord
	for _, directory := range analysis.Scope.Ports {
		// A record whose evaluation failed is tried again: what kept it
		// may have been this Mac's, such as MacPorts busy or a timeout
		// (Codex's review of 386ac2cc, finding 5).
		if recorded, ok := records[directory]; ok && recorded.Problem == "" {
			continue
		}
		if !e.mayEvaluate(ctx, branch, "change records") {
			break
		}
		var sides [2]*macports.Snapshot
		var texts [2][]byte
		problem := ""
		for i, source := range sources {
			file, text, err := e.Repo.File(ctx, string(source.Tree), directory+"/Portfile")
			if err != nil {
				return nil, err
			}
			if !file.Exists {
				continue
			}
			texts[i] = text
			snapshot, err := family.Family(ctx, source, directory)
			if err != nil {
				problem = sideWords[i] + ": " + err.Error()
				break
			}
			sides[i] = &snapshot
		}
		record := changeRecord(model.ChangeRecord{Branch: id, Tree: tree, Base: base, Directory: directory, At: e.now()}, sides, texts, problem)
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

// changedSubports are the subports of a directory a revision may change,
// as its record says, and whether it says: a directory with no record has
// its text scope, every subport, and a record never narrows further than
// it covered (model.ChangeRecord.Scope). One that finds no subport
// changed though its files did keeps the text scope: the change is in
// what this Mac's evaluation doesn't see, such as a block for another
// macOS release (the multi-subport sweep: openssh's for macOS 27,
// py-tkinter's for older ones). A subport whose text changed only in
// such a block keeps it too, beside those the record finds changed
// (Codex's review of 386ac2cc, finding 1).
func changedSubports(records map[string]model.ChangeRecord, directory string) ([]string, bool) {
	record, ok := records[directory]
	if !ok {
		return nil, false
	}
	return record.Scope()
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
// may have changed, with a note of why: its record isn't made yet,
// couldn't be made and why, or found nothing this Mac's evaluation sees.
func recordedPorts(directories []string, records map[string]model.ChangeRecord) (ports []string, notes map[string]string) {
	notes = map[string]string{}
	for _, directory := range directories {
		record, recorded := records[directory]
		if changed, ok := changedSubports(records, directory); ok {
			ports = append(ports, changed...)
			for _, port := range changed {
				if slices.Contains(record.Unseen, port) && !slices.Contains(record.Changed(), port) {
					notes[port] = "changed where this Mac's evaluation doesn't look"
				}
			}
			continue
		}
		port := path.Base(directory)
		ports = append(ports, port)
		switch {
		case !recorded:
			notes[port] = "not yet evaluated"
		case record.Problem != "":
			// "the base couldn't be evaluated: …", or the revision.
			notes[port] = record.Problem
		case record.AllUnseen && len(record.Changed()) > 0:
			notes[port] = "changed where this Mac's evaluation doesn't look, besides what it sees"
		default:
			notes[port] = "no change this Mac's evaluation sees"
		}
	}
	return ports, notes
}

// changeRecord is the one way a change record is made (L2's one-producer
// half): record's branch, files, base, directory, and time, with what the
// two sides' evaluations and Portfile texts say, base first, a side the
// revision hasn't nil; the family evaluation's, or an update's own. A
// problem that kept a side from being evaluated is the record's, which
// then says nothing of its subports, and its readers take the directory's
// text scope.
func changeRecord(record model.ChangeRecord, sides [2]*macports.Snapshot, texts [2][]byte, problem string) model.ChangeRecord {
	record.Policy, record.Problem = fidelity.ChangePolicy, problem
	for _, side := range sides {
		if side != nil {
			record.Platform, record.Runtime = side.Platform, side.Runtime.Provenance()
		}
	}
	if problem != "" {
		return record
	}
	record.Ports = fidelity.SubportChanges(record.Directory, sides[0], sides[1])
	record.Unseen, record.AllUnseen = portfile.UnseenChanges(texts[0], texts[1])
	return record
}

// PlanSiblings are a plan's changed targets that their directory's change
// record finds unchanged: built because a check builds every subport of a
// changed Portfile, as MacPorts' CI does, not because the revision changed
// them, as terraform-1.16's 18 siblings (field testing's terraform replay,
// 2026-10-22). None for a directory no record narrows, which keeps its
// text scope (changedSubports), or where the plan's revision can't be read.
// The records are made where they aren't yet, as a plan of a new commit's
// files has none: reading only what was recorded, check --plan on one
// said "Changed 19 ports" still (the replay's rerun on 1f445dc7).
func (e *Engine) PlanSiblings(ctx context.Context, plan model.Plan) map[model.TargetID]bool {
	revision, err := e.Revision(ctx, plan.Revision)
	if err != nil || revision.Branch == "" {
		return nil
	}
	records, err := e.revisionChanges(ctx, revision.Branch, revision.Source.Base, revision.Source.Tree, true)
	if err != nil {
		return nil
	}
	return planSiblings(plan, records)
}

// planSiblings are the plan's changed targets their directories' records
// find unchanged (PlanSiblings).
func planSiblings(plan model.Plan, records map[string]model.ChangeRecord) map[model.TargetID]bool {
	siblings := map[model.TargetID]bool{}
	for _, target := range plan.Targets {
		if target.Role != model.Changed {
			continue
		}
		if changed, ok := changedSubports(records, target.Directory); ok && !slices.Contains(changed, target.Target.Name) {
			siblings[target.ID] = true
		}
	}
	return siblings
}
