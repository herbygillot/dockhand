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

func (t *transaction) PublicationForJob(ctx context.Context, id record.JobID) (record.PublicationAction, error) {
	var v record.PublicationAction
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var raw string
	var confirmed sql.NullInt64
	err := t.conn.QueryRowContext(ctx, "SELECT id,job_id,change_id,revision_id,spec,state,push_started,write_started,confirmed_at,last_error FROM publications WHERE repository_id=? AND job_id=?", t.repo, id).Scan(&v.ID, &v.JobID, &v.ChangeID, &v.RevisionID, &raw, &v.State, &v.PushStarted, &v.WriteStarted, &confirmed, &v.LastError)
	if err != nil {
		return v, storageError(err)
	}
	v.ConfirmedAt = scanTime(confirmed)
	return v, decode(raw, &v.Spec)
}

func (t *transaction) PutPublication(ctx context.Context, v record.PublicationAction) error {
	if v.ID == "" || v.JobID == "" || v.ChangeID == "" || v.RevisionID == "" || v.Spec.EvidenceAttempt == "" {
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
		if old.ID != v.ID || old.ChangeID != v.ChangeID || old.RevisionID != v.RevisionID || !reflect.DeepEqual(old.Spec, v.Spec) || old.PushStarted && !v.PushStarted || old.WriteStarted && !v.WriteStarted {
			return state.ErrConflict
		}
		if old.State == record.PublicationConfirmed || old.State == record.PublicationNeedsAttention {
			return immutable(old, v)
		}
		return t.exec(ctx, "UPDATE publications SET state=?,push_started=?,write_started=?,confirmed_at=?,last_error=? WHERE repository_id=? AND id=?", v.State, v.PushStarted, v.WriteStarted, nullableTime(v.ConfirmedAt), v.LastError, t.repo, v.ID)
	}
	if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	if v.State != record.PublicationPending || v.PushStarted || v.WriteStarted {
		return state.ErrInvalid
	}
	raw, err := encode(v.Spec)
	if err != nil {
		return err
	}
	return t.exec(ctx, "INSERT INTO publications(id,repository_id,job_id,change_id,revision_id,evidence_attempt,forge,head_repository,head_branch,spec,state,push_started,write_started,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,0,0,?)", v.ID, t.repo, v.JobID, v.ChangeID, v.RevisionID, v.Spec.EvidenceAttempt, v.Spec.Forge, v.Spec.HeadRepository, v.Spec.HeadBranch, raw, v.State, v.LastError)
}

type pullRequestObservation struct {
	HeadRepository, HeadBranch, BaseBranch, URL string
	State                                       record.PullRequestState
	RemoteHead                                  record.ObjectID
	Title, Body                                 string
	ObservedAt                                  time.Time
}

func (t *transaction) PullRequest(ctx context.Context, id record.PullRequestID) (record.PullRequest, error) {
	var v record.PullRequest
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var raw string
	err := t.conn.QueryRowContext(ctx, "SELECT id,change_id,forge,remote_repository,number,observation FROM pull_requests WHERE repository_id=? AND id=?", t.repo, id).Scan(&v.ID, &v.ChangeID, &v.Ref.Forge, &v.Ref.Repository, &v.Ref.Number, &raw)
	if err != nil {
		return v, storageError(err)
	}
	var observation pullRequestObservation
	if err := decode(raw, &observation); err != nil {
		return v, err
	}
	v.HeadRepository, v.HeadBranch, v.BaseBranch, v.Ref.URL = observation.HeadRepository, observation.HeadBranch, observation.BaseBranch, observation.URL
	v.State, v.RemoteHead, v.Title, v.Body, v.ObservedAt = observation.State, observation.RemoteHead, observation.Title, observation.Body, observation.ObservedAt
	return v, nil
}

func (t *transaction) PutPullRequest(ctx context.Context, v record.PullRequest) error {
	if v.ID == "" || v.ChangeID == "" || v.Ref.Forge == "" || v.Ref.Repository == "" || v.Ref.Number <= 0 || v.Ref.URL == "" || !objectID(v.RemoteHead) || v.ObservedAt.IsZero() {
		return state.ErrInvalid
	}
	if _, err := t.Change(ctx, v.ChangeID); err != nil {
		return err
	}
	raw, err := encode(pullRequestObservation{HeadRepository: v.HeadRepository, HeadBranch: v.HeadBranch, BaseBranch: v.BaseBranch, URL: v.Ref.URL, State: v.State, RemoteHead: v.RemoteHead, Title: v.Title, Body: v.Body, ObservedAt: v.ObservedAt})
	if err != nil {
		return err
	}
	old, err := t.PullRequest(ctx, v.ID)
	if err == nil {
		if old.ChangeID != v.ChangeID || old.Ref != v.Ref || v.ObservedAt.Before(old.ObservedAt) {
			return state.ErrConflict
		}
		return t.exec(ctx, "UPDATE pull_requests SET observation=? WHERE repository_id=? AND id=?", raw, t.repo, v.ID)
	}
	if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	return t.exec(ctx, "INSERT INTO pull_requests(id,repository_id,change_id,forge,remote_repository,number,observation) VALUES(?,?,?,?,?,?,?)", v.ID, t.repo, v.ChangeID, v.Ref.Forge, v.Ref.Repository, v.Ref.Number, raw)
}
