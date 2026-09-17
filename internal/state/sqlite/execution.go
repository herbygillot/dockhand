package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"reflect"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

type buildOptions struct {
	ReplaceRemoteHead record.ObjectID `json:",omitempty"`
	Preinstall        []record.Target `json:",omitempty"`
	Branch            string          `json:",omitempty"`
	RemoteBranch      string          `json:",omitempty"`
	Target            record.Target
	Config            record.BuildConfig
	Inputs            []record.Artifact
}

func (t *transaction) Attempt(ctx context.Context, id record.AttemptID) (record.Attempt, error) {
	var v record.Attempt
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var source, raw string
	var owner sql.NullString
	var claimUntil, retry, cancel sql.NullInt64
	var created int64
	err := t.conn.QueryRowContext(ctx, `SELECT id,job_id,target_id,coalesce(revision_id,''),source_id,build,state,claim_owner,claim_generation,claim_until,retry_at,cancel_sent_at,cancel_observe,last_error,created_at,consecutive_failures,consecutive_waits,wait_kind FROM attempts WHERE repository_id=? AND id=?`, t.repo, id).Scan(&v.ID, &v.JobID, &v.TargetID, &v.Spec.RevisionID, &source, &raw, &v.State, &owner, &v.ClaimGeneration, &claimUntil, &retry, &cancel, &v.CancelPendingObservation, &v.LastError, &created, &v.ConsecutiveFailures, &v.ConsecutiveWaits, &v.WaitKind)
	if err != nil {
		return v, storageError(err)
	}
	var build buildOptions
	if err = decode(raw, &build); err != nil {
		return v, err
	}
	v.Spec.Branch, v.Spec.RemoteBranch = build.Branch, build.RemoteBranch
	v.Spec.Target, v.Spec.Config, v.Spec.Inputs = build.Target, build.Config, build.Inputs
	v.Spec.Preinstall = build.Preinstall
	v.Spec.ReplaceRemoteHead = build.ReplaceRemoteHead
	if v.Spec.Source, err = t.readSource(ctx, source); err != nil {
		return v, err
	}
	v.CreatedAt = fromTime(created)
	v.Claim = readClaim(owner, v.ClaimGeneration, claimUntil)
	v.RetryAt = scanTime(retry)
	v.CancelSentAt = scanTime(cancel)
	err = t.conn.QueryRowContext(ctx, "SELECT evidence FROM attempt_evidence WHERE repository_id=? AND attempt_id=?", t.repo, id).Scan(&raw)
	if err == nil {
		v.Evidence = &record.Evidence{}
		if err = decode(raw, v.Evidence); err != nil {
			return v, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return v, storageError(err)
	}
	var submission record.RequestID
	err = t.conn.QueryRowContext(ctx, "SELECT id FROM submissions WHERE repository_id=? AND attempt_id=? ORDER BY sequence DESC LIMIT 1", t.repo, id).Scan(&submission)
	if err == nil {
		s, e := t.Submission(ctx, submission)
		if e != nil {
			return v, e
		}
		v.SubmissionID = s.ID
		if s.RunID != "" {
			v.Run = record.ProviderRun{Provider: s.Provider, RequestID: s.ID, RunID: s.RunID}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return v, storageError(err)
	}
	return v, nil
}
func (t *transaction) PutAttempt(ctx context.Context, v record.Attempt) error {
	if v.ID == "" || v.JobID == "" || v.TargetID == "" || v.CreatedAt.IsZero() {
		return state.ErrInvalid
	}
	owner, until, err := claimValues(v.Claim, v.ClaimGeneration)
	if err != nil {
		return err
	}
	old, err := t.Attempt(ctx, v.ID)
	found := err == nil
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return err
	}
	if found {
		if old.SubmissionID != v.SubmissionID || old.Run != v.Run {
			return state.ErrConflict
		}
		if old.JobID != v.JobID || old.TargetID != v.TargetID || !old.CreatedAt.Equal(v.CreatedAt) || old.ClaimGeneration > v.ClaimGeneration {
			return state.ErrConflict
		}
		if err = immutable(old.Spec, v.Spec); err != nil {
			return err
		}
		if old.State == record.AttemptFinished || old.State == record.AttemptCanceled {
			if old.State != v.State || !reflect.DeepEqual(old.Evidence, v.Evidence) {
				return state.ErrConflict
			}
		}
		if old.Evidence != nil && v.Evidence != nil && v.Evidence.ObservedAt.Before(old.Evidence.ObservedAt) {
			return state.ErrConflict
		}
	}
	var next any = nextTime(v.Claim, v.RetryAt)
	if v.State == record.AttemptFinished || v.State == record.AttemptCanceled {
		next = nil
	}
	if found {
		err = t.exec(ctx, `UPDATE attempts SET state=?,claim_owner=?,claim_generation=?,claim_until=?,consecutive_failures=?,consecutive_waits=?,wait_kind=?,retry_at=?,next_action_at=?,cancel_sent_at=?,cancel_observe=?,last_error=? WHERE repository_id=? AND id=?`, v.State, owner, v.ClaimGeneration, until, v.ConsecutiveFailures, v.ConsecutiveWaits, v.WaitKind, nullableTime(v.RetryAt), next, nullableTime(v.CancelSentAt), v.CancelPendingObservation, v.LastError, t.repo, v.ID)
	} else {
		if v.Spec.RevisionID != "" {
			revision, e := t.Revision(ctx, v.Spec.RevisionID)
			if e != nil {
				return e
			}
			if revision.Source != v.Spec.Source {
				return state.ErrConflict
			}
		}
		job, e := t.Job(ctx, v.JobID)
		if e != nil {
			return e
		}
		if (v.Spec.RevisionID == "" && (job.Spec.Action != record.Verify || job.Spec.InputRevision != "" || job.ResultRevision != "" || job.Spec.Source != v.Spec.Source)) || (v.Spec.RevisionID != "" && job.Spec.InputRevision != v.Spec.RevisionID && job.ResultRevision != v.Spec.RevisionID) {
			return state.ErrConflict
		}
		source, e := t.source(ctx, v.Spec.Source)
		if e != nil {
			return e
		}
		raw, e := encode(buildOptions{ReplaceRemoteHead: v.Spec.ReplaceRemoteHead, Preinstall: v.Spec.Preinstall, Branch: v.Spec.Branch, RemoteBranch: v.Spec.RemoteBranch, Target: v.Spec.Target, Config: v.Spec.Config, Inputs: v.Spec.Inputs})
		if e != nil {
			return e
		}
		err = t.exec(ctx, `INSERT INTO attempts(id,repository_id,job_id,target_id,revision_id,source_id,build,state,claim_owner,claim_generation,claim_until,consecutive_failures,consecutive_waits,wait_kind,retry_at,next_action_at,cancel_sent_at,cancel_observe,last_error,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, t.repo, v.JobID, v.TargetID, nullableID(v.Spec.RevisionID), source, raw, v.State, owner, v.ClaimGeneration, until, v.ConsecutiveFailures, v.ConsecutiveWaits, v.WaitKind, nullableTime(v.RetryAt), next, nullableTime(v.CancelSentAt), v.CancelPendingObservation, v.LastError, v.CreatedAt.UnixMilli())
	}
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(old.Evidence, v.Evidence) {
		if v.Evidence == nil {
			return state.ErrConflict
		}
		raw, e := encode(v.Evidence)
		if e != nil {
			return e
		}
		if err = t.exec(ctx, "INSERT INTO attempt_evidence(repository_id,attempt_id,evidence) VALUES(?,?,?) ON CONFLICT(attempt_id) DO UPDATE SET evidence=excluded.evidence", t.repo, v.ID, raw); err != nil {
			return err
		}
	}
	return t.scheduleJob(ctx, v.JobID)
}
func (t *transaction) Submission(ctx context.Context, id record.RequestID) (record.Submission, error) {
	var v record.Submission
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var run sql.NullString
	var created int64
	var admitted, closed sql.NullInt64
	err := t.conn.QueryRowContext(ctx, "SELECT id,attempt_id,sequence,provider,run_id,created_at,admitted_at,closed_at FROM submissions WHERE repository_id=? AND id=?", t.repo, id).Scan(&v.ID, &v.AttemptID, &v.Sequence, &v.Provider, &run, &created, &admitted, &closed)
	v.RunID = run.String
	v.CreatedAt = fromTime(created)
	v.AdmittedAt = scanTime(admitted)
	v.ClosedAt = scanTime(closed)
	return v, storageError(err)
}
func (t *transaction) PutSubmission(ctx context.Context, v record.Submission) error {
	if v.ID == "" || v.AttemptID == "" || v.Provider == "" || v.Sequence < 1 || v.CreatedAt.IsZero() {
		return state.ErrInvalid
	}
	old, err := t.Submission(ctx, v.ID)
	if err == nil {
		if old.AttemptID != v.AttemptID || old.Sequence != v.Sequence || old.Provider != v.Provider || !old.CreatedAt.Equal(v.CreatedAt) || (old.RunID != "" && old.RunID != v.RunID) || (old.ClosedAt != nil && !reflect.DeepEqual(old.ClosedAt, v.ClosedAt)) || (old.AdmittedAt != nil && !reflect.DeepEqual(old.AdmittedAt, v.AdmittedAt)) {
			return state.ErrConflict
		}
		return t.exec(ctx, "UPDATE submissions SET run_id=?,admitted_at=?,closed_at=? WHERE repository_id=? AND id=?", nullableID(v.RunID), nullableTime(v.AdmittedAt), nullableTime(v.ClosedAt), t.repo, v.ID)
	}
	if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	attempt, err := t.Attempt(ctx, v.AttemptID)
	if err != nil {
		return err
	}
	if attempt.Spec.Config.Provider != v.Provider {
		return state.ErrConflict
	}
	return t.exec(ctx, "INSERT INTO submissions(id,repository_id,attempt_id,sequence,provider,run_id,created_at,admitted_at,closed_at) VALUES(?,?,?,?,?,?,?,?,?)", v.ID, t.repo, v.AttemptID, v.Sequence, v.Provider, nullableID(v.RunID), v.CreatedAt.UnixMilli(), nullableTime(v.AdmittedAt), nullableTime(v.ClosedAt))
}
func (t *transaction) Resource(ctx context.Context, id record.ResourceID) (record.Resource, error) {
	var v record.Resource
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var owner sql.NullString
	var until, retry, retain, released, pruned sql.NullInt64
	err := t.conn.QueryRowContext(ctx, `SELECT id,attempt_id,submission_id,provider,handle,state,claim_owner,claim_generation,claim_until,retry_at,retain_until,released_at,artifacts_pruned_at,last_error,consecutive_failures,consecutive_waits,wait_kind FROM resources WHERE repository_id=? AND id=?`, t.repo, id).Scan(&v.ID, &v.AttemptID, &v.SubmissionID, &v.Handle.Provider, &v.Handle.ID, &v.State, &owner, &v.ClaimGeneration, &until, &retry, &retain, &released, &pruned, &v.LastError, &v.ConsecutiveFailures, &v.ConsecutiveWaits, &v.WaitKind)
	v.Claim = readClaim(owner, v.ClaimGeneration, until)
	v.RetryAt = scanTime(retry)
	v.RetainUntil = scanTime(retain)
	v.ReleasedAt = scanTime(released)
	v.ArtifactsPrunedAt = scanTime(pruned)
	return v, storageError(err)
}
func (t *transaction) PutResource(ctx context.Context, v record.Resource) error {
	if v.ID == "" || v.AttemptID == "" || v.SubmissionID == "" || v.Handle.Provider == "" || v.Handle.ID == "" {
		return state.ErrInvalid
	}
	if v.ArtifactsPrunedAt != nil && (v.State != record.ResourceReleased || v.ReleasedAt == nil || v.ArtifactsPrunedAt.Before(*v.ReleasedAt)) {
		return state.ErrInvalid
	}
	owner, until, err := claimValues(v.Claim, v.ClaimGeneration)
	if err != nil {
		return err
	}
	old, err := t.Resource(ctx, v.ID)
	found := err == nil
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return err
	}
	if found {
		if old.AttemptID != v.AttemptID || old.SubmissionID != v.SubmissionID || old.Handle != v.Handle || old.ClaimGeneration > v.ClaimGeneration {
			return state.ErrConflict
		}
		if old.State == record.ResourceReleased {
			compared := v
			compared.ArtifactsPrunedAt = old.ArtifactsPrunedAt
			compared.RetryAt, compared.LastError = old.RetryAt, old.LastError
			if err := immutable(old, compared); err != nil {
				return err
			}
			if old.ArtifactsPrunedAt != nil {
				return immutable(old, v)
			}
			return t.exec(ctx, "UPDATE resources SET artifacts_pruned_at=?,retry_at=?,last_error=? WHERE repository_id=? AND id=?", nullableTime(v.ArtifactsPrunedAt), nullableTime(v.RetryAt), v.LastError, t.repo, v.ID)
		}
	} else {
		submission, e := t.Submission(ctx, v.SubmissionID)
		if e != nil {
			return e
		}
		if submission.AttemptID != v.AttemptID || submission.Provider != v.Handle.Provider {
			return state.ErrConflict
		}
	}
	if v.ArtifactsPrunedAt != nil {
		return state.ErrInvalid
	}
	var next any = nextTime(v.Claim, v.RetryAt)
	switch v.State {
	case record.ResourceReleased, record.ResourceActive:
		next = nil
	case record.ResourceRetained:
		if v.RetainUntil == nil {
			next = nil
		} else {
			next = max(next.(int64), v.RetainUntil.UnixMilli())
		}
	}
	if found {
		return t.exec(ctx, `UPDATE resources SET state=?,claim_owner=?,claim_generation=?,claim_until=?,consecutive_failures=?,consecutive_waits=?,wait_kind=?,retry_at=?,next_action_at=?,retain_until=?,released_at=?,last_error=? WHERE repository_id=? AND id=?`, v.State, owner, v.ClaimGeneration, until, v.ConsecutiveFailures, v.ConsecutiveWaits, v.WaitKind, nullableTime(v.RetryAt), next, nullableTime(v.RetainUntil), nullableTime(v.ReleasedAt), v.LastError, t.repo, v.ID)
	}
	return t.exec(ctx, `INSERT INTO resources(id,repository_id,attempt_id,submission_id,provider,handle,state,claim_owner,claim_generation,claim_until,consecutive_failures,consecutive_waits,wait_kind,retry_at,next_action_at,retain_until,released_at,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, t.repo, v.AttemptID, v.SubmissionID, v.Handle.Provider, v.Handle.ID, v.State, owner, v.ClaimGeneration, until, v.ConsecutiveFailures, v.ConsecutiveWaits, v.WaitKind, nullableTime(v.RetryAt), next, nullableTime(v.RetainUntil), nullableTime(v.ReleasedAt), v.LastError)
}
