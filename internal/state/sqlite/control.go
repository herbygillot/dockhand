package sqlite

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

func (t *transaction) Control(ctx context.Context, id record.RequestID) (record.ControlRequest, error) {
	r, err := t.Request(ctx, id)
	if err != nil {
		return record.ControlRequest{}, err
	}
	if r.Kind != record.CancelRequest {
		return record.ControlRequest{}, state.ErrNotFound
	}
	v := record.ControlRequest{ID: id, Kind: record.Cancel, SubmittedAt: r.AcceptedAt, AppliedAt: r.CompletedAt, Jobs: []record.JobID{}}
	if err = decode(string(r.Payload), &v.Reason); err != nil {
		return v, err
	}
	ids, err := t.ids(ctx, "SELECT job_id FROM control_jobs WHERE repository_id=? AND request_id=? ORDER BY job_id", t.repo, id)
	for _, id := range ids {
		v.Jobs = append(v.Jobs, record.JobID(id))
	}
	return v, err
}
func (t *transaction) PutControl(ctx context.Context, v record.ControlRequest) error {
	if v.ID == "" || v.Kind != record.Cancel || v.SubmittedAt.IsZero() || len(v.Jobs) == 0 || v.AppliedAt != nil {
		return state.ErrInvalid
	}
	jobs := slices.Clone(v.Jobs)
	slices.Sort(jobs)
	v.Jobs = slices.Compact(jobs)
	if old, err := t.Control(ctx, v.ID); err == nil {
		return immutable(old, v)
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	raw, err := encode(v.Reason)
	if err != nil {
		return err
	}
	if err = t.PutRequest(ctx, record.AcceptedRequest{ID: v.ID, Kind: record.CancelRequest, Payload: []byte(raw), AcceptedAt: v.SubmittedAt}); err != nil {
		return err
	}
	for _, id := range v.Jobs {
		if err = t.exec(ctx, "INSERT INTO control_jobs(repository_id,request_id,job_id) VALUES(?,?,?)", t.repo, v.ID, id); err != nil {
			return err
		}
	}
	return nil
}
func (t *transaction) ApplyControl(ctx context.Context, id record.RequestID, job record.JobID, at time.Time) error {
	if at.IsZero() {
		return state.ErrInvalid
	}
	if err := t.exec(ctx, "UPDATE control_jobs SET applied_at=coalesce(applied_at,?) WHERE repository_id=? AND request_id=? AND job_id=?", at.UnixMilli(), t.repo, id, job); err != nil {
		return err
	}
	return t.exec(ctx, `UPDATE requests SET completed_at=coalesce(completed_at,?) WHERE repository_id=? AND id=? AND kind='cancel' AND NOT EXISTS(SELECT 1 FROM control_jobs c WHERE c.repository_id=requests.repository_id AND c.request_id=requests.id AND c.applied_at IS NULL)`, at.UnixMilli(), t.repo, id)
}
