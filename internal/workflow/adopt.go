package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

type BranchInput struct {
	Name             string
	ExpectedChange   record.ChangeID
	ExpectedRevision record.RevisionID
	// InferredTarget is the recorded contribution target used for inference.
	// Acceptance rechecks it even if the contribution revision has not changed.
	InferredTarget *record.Target `json:",omitempty"`
}

func validateBranchInput(branch *BranchInput, spec record.JobSpec) error {
	if branch == nil {
		return nil
	}
	if !git.ValidBranchName(branch.Name) || (spec.Action != record.Verify && spec.Action != record.Publish) || spec.InputRevision != "" || spec.ChangeID != "" || len(spec.Targets) != 1 || spec.Build == nil || (branch.ExpectedChange == "") != (branch.ExpectedRevision == "") {
		return fmt.Errorf("%w: branch adoption requires one frozen verification input and matching revision preconditions", ErrInvalidRequest)
	}
	if branch.InferredTarget != nil && (spec.Action != record.Verify || branch.ExpectedChange == "" || branch.InferredTarget.Portfile != spec.Targets[0].Portfile) {
		return fmt.Errorf("%w: inferred verification requires a tracked contribution target", ErrInvalidRequest)
	}
	if spec.Action == record.Publish && (spec.Source.Commit == "" || spec.Source.Base == "" || spec.Publication == nil || spec.Publication.SourceBranch() != branch.Name) {
		return ErrInvalidRequest
	}
	if spec.Checkout != nil && spec.Checkout.Branch != branch.Name {
		return fmt.Errorf("%w: checkout branch disagrees with binding", ErrInvalidRequest)
	}
	if branch.ExpectedChange != "" && (!validToken(string(branch.ExpectedChange)) || !validToken(string(branch.ExpectedRevision))) {
		return ErrInvalidRequest
	}
	return nil
}

func adoptBranch(ctx context.Context, tx state.Tx, spec record.JobSpec, input BranchInput, now time.Time) (record.JobSpec, error) {
	change, err := tx.OpenChangeByBranch(ctx, input.Name)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return record.JobSpec{}, err
	}
	if change.ID != input.ExpectedChange || change.CurrentRevision != input.ExpectedRevision {
		return record.JobSpec{}, fmt.Errorf("%w: tracked branch %s changed while binding", ErrStaleRevision, input.Name)
	}
	if input.InferredTarget != nil && (len(change.Targets) != 1 || record.CompareTargets(change.Targets[0], *input.InferredTarget) != 0) {
		return record.JobSpec{}, fmt.Errorf("%w: tracked targets changed while binding; run verify again", ErrStaleRevision)
	}
	if change.ID == "" && spec.Action != record.Publish {
		return spec, nil
	}
	var previous record.Revision
	if change.ID == "" {
		change = record.Change{ID: record.ChangeID("change_" + rand.Text()), Branch: input.Name, Targets: spec.Targets, Disposition: record.ChangeOpen, CreatedAt: now}
		if err := tx.PutChange(ctx, change); err != nil {
			return record.JobSpec{}, err
		}
	} else {
		previous, err = tx.Revision(ctx, change.CurrentRevision)
		if err != nil {
			return record.JobSpec{}, err
		}
	}
	if change.ID != "" {
		if err := correctionNotPending(ctx, tx, change.ID); err != nil {
			return record.JobSpec{}, err
		}
	}
	revision := previous
	if revision.ID == "" || revision.Source != spec.Source {
		revision = record.Revision{ID: record.RevisionID("revision_" + rand.Text()), ChangeID: change.ID, Previous: change.CurrentRevision, Source: spec.Source, CreatedAt: now}
		if err := tx.PutRevision(ctx, revision); err != nil {
			return record.JobSpec{}, err
		}
		change.CurrentRevision = revision.ID
		if err := tx.PutChange(ctx, change); err != nil {
			return record.JobSpec{}, err
		}
	}
	spec.ChangeID, spec.InputRevision = change.ID, revision.ID
	return spec, nil
}
