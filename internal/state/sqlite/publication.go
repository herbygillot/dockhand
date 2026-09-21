package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

const publicationColumns = "SELECT id,job_id,change_id,revision_id,spec,state,push_started,write_started,confirmed_at,last_error,write_refusals FROM publications WHERE repository_id=? AND "

func (t *transaction) PublicationForJob(ctx context.Context, id record.JobID) (record.PublicationAction, error) {
	return t.publication(ctx, "job_id=?", id)
}

// ActivePublicationForHead finds the publication that holds a remote head
// branch: one still pending, applying, or uncertain, which is the set the
// unique index guards.
func (t *transaction) ActivePublicationForHead(ctx context.Context, forge, headRepository, headBranch string) (record.PublicationAction, error) {
	return t.publication(ctx, "forge=? AND head_repository=? AND head_branch=? AND state IN ('pending','applying','uncertain')", forge, headRepository, headBranch)
}

func (t *transaction) publication(ctx context.Context, where string, args ...any) (record.PublicationAction, error) {
	var v record.PublicationAction
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var raw string
	var confirmed sql.NullInt64
	err := t.conn.QueryRowContext(ctx, publicationColumns+where, append([]any{t.repo}, args...)...).Scan(&v.ID, &v.JobID, &v.ChangeID, &v.RevisionID, &raw, &v.State, &v.PushStarted, &v.WriteStarted, &confirmed, &v.LastError, &v.WriteRefusals)
	if err != nil {
		return v, storageError(err)
	}
	v.ConfirmedAt = scanTime(confirmed)
	return v, decode(raw, &v.Spec)
}

func (t *transaction) PutPublication(ctx context.Context, v record.PublicationAction) error {
	if v.ID == "" || v.JobID == "" || v.ChangeID == "" || v.RevisionID == "" || (v.Spec.EvidenceAttempt == "") != v.Spec.Unverified {
		return state.ErrInvalid
	}
	job, err := t.Job(ctx, v.JobID)
	if err != nil {
		return err
	}
	if job.ChangeID != v.ChangeID || job.Phase != record.PhasePublication {
		return state.ErrConflict
	}
	old, err := t.PublicationForJob(ctx, v.JobID)
	if err == nil {
		if old.ID != v.ID || old.ChangeID != v.ChangeID || old.RevisionID != v.RevisionID || !reflect.DeepEqual(old.Spec, v.Spec) || old.PushStarted && !v.PushStarted {
			return state.ErrConflict
		}
		cleared := old.WriteStarted && !v.WriteStarted
		if cleared {
			if old.WriteRefusals == ^uint32(0) || v.WriteRefusals != old.WriteRefusals+1 || v.State != record.PublicationPending || v.LastError == "" {
				return state.ErrConflict
			}
		} else if old.WriteRefusals != v.WriteRefusals {
			return state.ErrConflict
		}
		if old.State == record.PublicationConfirmed || old.State == record.PublicationNeedsAttention {
			return immutable(old, v)
		}
		return t.exec(ctx, "UPDATE publications SET state=?,push_started=?,write_started=?,confirmed_at=?,last_error=?,write_refusals=? WHERE repository_id=? AND id=?", v.State, v.PushStarted, v.WriteStarted, nullableTime(v.ConfirmedAt), v.LastError, v.WriteRefusals, t.repo, v.ID)
	}
	if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	if v.State != record.PublicationPending || v.PushStarted || v.WriteStarted || v.WriteRefusals != 0 {
		return state.ErrInvalid
	}
	raw, err := encode(v.Spec)
	if err != nil {
		return err
	}
	return t.exec(ctx, "INSERT INTO publications(id,repository_id,job_id,change_id,revision_id,evidence_attempt,forge,head_repository,head_branch,spec,state,push_started,write_started,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,0,0,?)", v.ID, t.repo, v.JobID, v.ChangeID, v.RevisionID, nullableID(v.Spec.EvidenceAttempt), v.Spec.Forge, v.Spec.HeadRepository, v.Spec.HeadBranch, raw, v.State, v.LastError)
}

type pullRequestObservation struct {
	HeadRepository, HeadBranch, BaseBranch, URL string
	State                                       record.PullRequestState
	RemoteHead                                  record.ObjectID
	Title, Body                                 string
	ObservedAt                                  time.Time
	Status                                      *record.PullRequestStatus `json:",omitempty"`
}

func (t *transaction) PullRequest(ctx context.Context, id record.PullRequestID) (record.PullRequest, error) {
	var v record.PullRequest
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var raw string
	var observeAfter sql.NullInt64
	dests, err := pullRequests.scanArgs(map[string]any{"id": &v.ID, "change_id": &v.ChangeID, "forge": &v.Ref.Forge, "remote_repository": &v.Ref.Repository, "number": &v.Ref.Number, "observation": &raw, "observe_after": &observeAfter})
	if err != nil {
		return v, err
	}
	if err := t.conn.QueryRowContext(ctx, pullRequests.selectByID(), t.repo, id).Scan(dests...); err != nil {
		return v, storageError(err)
	}
	if observeAfter.Valid {
		at := fromTime(observeAfter.Int64)
		v.ObserveAfter = &at
	}
	var observation pullRequestObservation
	if err := decode(raw, &observation); err != nil {
		return v, err
	}
	v.HeadRepository, v.HeadBranch, v.BaseBranch, v.Ref.URL = observation.HeadRepository, observation.HeadBranch, observation.BaseBranch, observation.URL
	v.State, v.RemoteHead, v.Title, v.Body, v.ObservedAt, v.Status = observation.State, observation.RemoteHead, observation.Title, observation.Body, observation.ObservedAt, observation.Status
	return v, nil
}

func (t *transaction) PutPullRequest(ctx context.Context, v record.PullRequest) error {
	if v.ID == "" || v.ChangeID == "" || v.Ref.Forge == "" || v.Ref.Repository == "" || v.Ref.Number <= 0 || v.Ref.URL == "" || !objectID(v.RemoteHead) || v.ObservedAt.IsZero() {
		return state.ErrInvalid
	}
	if _, err := t.Change(ctx, v.ChangeID); err != nil {
		return err
	}
	raw, err := encode(pullRequestObservation{HeadRepository: v.HeadRepository, HeadBranch: v.HeadBranch, BaseBranch: v.BaseBranch, URL: v.Ref.URL, State: v.State, RemoteHead: v.RemoteHead, Title: v.Title, Body: v.Body, ObservedAt: v.ObservedAt, Status: v.Status})
	if err != nil {
		return err
	}
	named := map[string]any{"id": v.ID, "change_id": v.ChangeID, "forge": v.Ref.Forge, "remote_repository": v.Ref.Repository, "number": v.Ref.Number, "observation": raw, "observe_after": nullableTime(v.ObserveAfter)}
	old, err := t.PullRequest(ctx, v.ID)
	if err == nil {
		if old.ChangeID != v.ChangeID || old.Ref != v.Ref || v.ObservedAt.Before(old.ObservedAt) {
			return state.ErrConflict
		}
		updated := []string{"observation", "observe_after"}
		args, err := pullRequests.updateArgs(updated, named, t.repo, v.ID)
		if err != nil {
			return err
		}
		return t.exec(ctx, pullRequests.update(updated...), args...)
	}
	if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	args, err := pullRequests.insertArgs(t.repo, named)
	if err != nil {
		return err
	}
	return t.exec(ctx, pullRequests.insert(), args...)
}
