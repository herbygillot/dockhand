package workflow

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// acceptPreparation chooses the contribution while holding the state writer.
// It returns an existing job to join, or a new job retaining retry checkpoints.
func acceptPreparation(ctx context.Context, tx state.Tx, id record.JobID, request record.RequestID, spec record.JobSpec, now time.Time) (record.Job, *record.Job, error) {
	job := record.Job{ID: id, RequestID: request, Spec: spec, ChangeID: spec.ChangeID, State: record.JobQueued, Phase: initialPhase(spec.Action), AcceptedAt: now}
	if spec.Preparation == nil || spec.Preparation.Correction != nil {
		return job, nil, nil
	}
	var change record.Change
	if spec.ChangeID != "" {
		var err error
		change, err = tx.Change(ctx, spec.ChangeID)
		if err != nil {
			return job, nil, err
		}
		if change.Disposition != record.ChangeOpen || change.InitiatingTarget != spec.Targets[0].Name {
			return job, nil, ErrInvalidRequest
		}
	} else {
		matches, err := tx.Changes(ctx, state.Query{Target: spec.Targets[0].Name, Pending: true, Limit: 2})
		if err != nil {
			return job, nil, err
		}
		if len(matches) > 1 {
			return job, nil, fmt.Errorf("%w: multiple open contributions for %s; select --change", ErrInvalidRequest, spec.Targets[0].Name)
		}
		if len(matches) == 1 {
			change = matches[0]
		}
	}
	if change.ID == "" {
		change = record.Change{ID: record.ChangeID("change_" + string(id)), InitiatingTarget: spec.Targets[0].Name, Targets: spec.Targets, Disposition: record.ChangeOpen, CreatedAt: now}
		if err := tx.PutChange(ctx, change); err != nil {
			return job, nil, err
		}
	} else {
		previous, err := tx.Jobs(ctx, state.Query{ChangeID: change.ID, Newest: true, Action: spec.Action, Limit: 1})
		if err != nil {
			return job, nil, err
		}
		if len(previous) == 0 {
			return job, nil, fmt.Errorf("%w: %s already has contribution %s; continue it with verify or publish", ErrInvalidRequest, spec.Targets[0].Name, change.ID)
		}
		old := previous[0]
		if old.Spec.Action != spec.Action || !reflect.DeepEqual(old.Spec.Targets, spec.Targets) || old.Spec.Reason != spec.Reason {
			return job, nil, fmt.Errorf("%w: contribution %s already has a different preparation intent", ErrInvalidRequest, change.ID)
		}
		if old.Spec.Version != spec.Version && (old.ResolvedRelease != nil || old.Prepared != nil || !old.State.Terminal()) {
			if old.ResolvedRelease == nil || spec.Version != old.ResolvedRelease.Version && spec.Version != old.ResolvedRelease.Tag {
				return job, nil, fmt.Errorf("%w: contribution %s already selects another version; preserve or finish that contribution first", ErrInvalidRequest, change.ID)
			}
		}
		if change.CurrentRevision != "" && old.ResultRevision != change.CurrentRevision {
			return job, nil, fmt.Errorf("%w: contribution %s has advanced; use verify or publish", ErrStaleRevision, change.ID)
		}
		spec.Source = old.Spec.Source
		spec.EvaluatedVersions = old.Spec.EvaluatedVersions
		spec.ChangeID = change.ID
		compared, prior := spec, old.Spec
		prior.ChangeID = change.ID
		compared.Version = prior.Version
		pending, err := tx.Jobs(ctx, state.Query{ChangeID: change.ID, Pending: true, Limit: 1})
		if err != nil {
			return job, nil, err
		}
		if len(pending) > 0 && pending[0].ID != old.ID {
			return job, nil, fmt.Errorf("%w: contribution %s has pending work %s", ErrInvalidRequest, change.ID, pending[0].ID)
		}
		if !old.State.Terminal() {
			if !reflect.DeepEqual(compared, prior) {
				return job, nil, fmt.Errorf("%w: contribution %s has pending work with different settings", ErrInvalidRequest, change.ID)
			}
			return job, &old, nil
		}
		if old.State == record.JobCompleted && (reflect.DeepEqual(compared, prior) || spec.Destination == record.BranchReady) {
			return job, &old, nil
		}
		job.Spec = spec
		job.Prepared, job.ResultRevision = old.Prepared, old.ResultRevision
		if old.ResolvedRelease != nil {
			release := *old.ResolvedRelease
			release.Requested = spec.Version
			job.ResolvedRelease = &release
		}
		if job.ResultRevision != "" {
			job.Phase, job.State = record.PhaseVerification, record.JobActive
		}
	}
	job.ChangeID = change.ID
	job.Spec.ChangeID = change.ID
	return job, nil, nil
}

func closeEmptyContribution(ctx context.Context, tx state.Tx, id record.ChangeID) error {
	if id == "" {
		return nil
	}
	change, err := tx.Change(ctx, id)
	if err != nil {
		return err
	}
	if change.CurrentRevision != "" || change.Branch != "" {
		return nil
	}
	change.Disposition = record.ChangeClosed
	return tx.PutChange(ctx, change)
}
