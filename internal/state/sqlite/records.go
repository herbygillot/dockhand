package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

func (t *transaction) exec(ctx context.Context, query string, args ...any) error {
	if err := t.check(ctx, true); err != nil {
		return err
	}
	_, err := t.conn.ExecContext(ctx, query, args...)
	return storageError(err)
}
func encode(v any) (string, error) { b, e := json.Marshal(v); return string(b), e }
func decode(data string, v any) error {
	if err := json.Unmarshal([]byte(data), v); err != nil {
		return fmt.Errorf("%w: %v", state.ErrInvalid, err)
	}
	return nil
}
func immutable(old, new any) error {
	if !reflect.DeepEqual(old, new) {
		return fmt.Errorf("%w: immutable record changed", state.ErrConflict)
	}
	return nil
}
func objectID(s record.ObjectID) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	_, e := hex.DecodeString(string(s))
	return e == nil && strings.ToLower(string(s)) == string(s)
}
func (t *transaction) source(ctx context.Context, s record.Source) (string, error) {
	if !objectID(s.Tree) {
		return "", state.ErrInvalid
	}
	for _, v := range []record.ObjectID{s.Commit, s.Base} {
		if v != "" && (!objectID(v) || len(v) != len(s.Tree)) {
			return "", state.ErrInvalid
		}
	}
	raw, _ := encode(s)
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
	err := t.exec(ctx, "INSERT INTO sources(repository_id,id,commit_id,tree_id,base_id) VALUES(?,?,?,?,?) ON CONFLICT(repository_id,id) DO NOTHING", t.repo, id, nullableID(s.Commit), s.Tree, nullableID(s.Base))
	return id, err
}
func (t *transaction) readSource(ctx context.Context, id string) (record.Source, error) {
	var s record.Source
	var commit, base sql.NullString
	err := t.conn.QueryRowContext(ctx, "SELECT commit_id,tree_id,base_id FROM sources WHERE repository_id=? AND id=?", t.repo, id).Scan(&commit, &s.Tree, &base)
	s.Commit = record.ObjectID(commit.String)
	s.Base = record.ObjectID(base.String)
	return s, storageError(err)
}

func (t *transaction) Change(ctx context.Context, id record.ChangeID) (record.Change, error) {
	var v record.Change
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var current sql.NullString
	var raw string
	var created int64
	err := t.conn.QueryRowContext(ctx, "SELECT id,branch,current_revision,disposition,targets,created_at,coalesce(published_revision,''),coalesce(pull_request_id,''),generated_commit FROM changes WHERE repository_id=? AND id=?", t.repo, id).Scan(&v.ID, &v.Branch, &current, &v.Disposition, &raw, &created, &v.PublishedRevision, &v.PullRequestID, &v.GeneratedCommit)
	if err != nil {
		return v, storageError(err)
	}
	v.CurrentRevision = record.RevisionID(current.String)
	v.CreatedAt = fromTime(created)
	return v, decode(raw, &v.Targets)
}
func (t *transaction) PutChange(ctx context.Context, v record.Change) error {
	if v.ID == "" {
		return state.ErrInvalid
	}
	if v.PublishedRevision != "" {
		rev, err := t.Revision(ctx, v.PublishedRevision)
		if err != nil {
			return err
		}
		if rev.ChangeID != v.ID {
			return state.ErrConflict
		}
	}
	if v.PullRequestID != "" {
		pr, err := t.PullRequest(ctx, v.PullRequestID)
		if err != nil {
			return err
		}
		if pr.ChangeID != v.ID {
			return state.ErrConflict
		}
	}
	old, err := t.Change(ctx, v.ID)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return err
	}
	if err == nil && (old.GeneratedCommit != v.GeneratedCommit || !old.CreatedAt.Equal(v.CreatedAt) || old.PullRequestID != "" && old.PullRequestID != v.PullRequestID) {
		return state.ErrConflict
	}
	raw, err := encode(v.Targets)
	if err != nil {
		return err
	}
	if old.ID != "" {
		return t.exec(ctx, "UPDATE changes SET branch=?,current_revision=?,disposition=?,targets=?,published_revision=?,pull_request_id=? WHERE repository_id=? AND id=?", v.Branch, nullableID(v.CurrentRevision), v.Disposition, raw, nullableID(v.PublishedRevision), nullableID(v.PullRequestID), t.repo, v.ID)
	}
	return t.exec(ctx, "INSERT INTO changes(id,repository_id,branch,current_revision,disposition,targets,created_at,generated_commit) VALUES(?,?,?,?,?,?,?,?)", v.ID, t.repo, v.Branch, nullableID(v.CurrentRevision), v.Disposition, raw, v.CreatedAt.UnixMilli(), v.GeneratedCommit)
}
func (t *transaction) Revision(ctx context.Context, id record.RevisionID) (record.Revision, error) {
	var v record.Revision
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var source string
	var previous sql.NullString
	var created int64
	err := t.conn.QueryRowContext(ctx, "SELECT id,change_id,source_id,previous_id,created_at FROM revisions WHERE repository_id=? AND id=?", t.repo, id).Scan(&v.ID, &v.ChangeID, &source, &previous, &created)
	if err != nil {
		return v, storageError(err)
	}
	v.Previous = record.RevisionID(previous.String)
	v.CreatedAt = fromTime(created)
	v.Source, err = t.readSource(ctx, source)
	return v, err
}
func (t *transaction) PutRevision(ctx context.Context, v record.Revision) error {
	if v.ID == "" || v.ChangeID == "" || v.ID == v.Previous {
		return state.ErrInvalid
	}
	if old, err := t.Revision(ctx, v.ID); err == nil {
		return immutable(old, v)
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	source, err := t.source(ctx, v.Source)
	if err != nil {
		return err
	}
	return t.exec(ctx, "INSERT INTO revisions(id,repository_id,change_id,source_id,previous_id,created_at) VALUES(?,?,?,?,?,?)", v.ID, t.repo, v.ChangeID, source, nullableID(v.Previous), v.CreatedAt.UnixMilli())
}
func (t *transaction) Request(ctx context.Context, id record.RequestID) (record.AcceptedRequest, error) {
	var v record.AcceptedRequest
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var accepted int64
	var completed sql.NullInt64
	err := t.conn.QueryRowContext(ctx, "SELECT id,kind,payload,accepted_at,completed_at FROM requests WHERE repository_id=? AND id=?", t.repo, id).Scan(&v.ID, &v.Kind, &v.Payload, &accepted, &completed)
	v.AcceptedAt = fromTime(accepted)
	v.CompletedAt = scanTime(completed)
	return v, storageError(err)
}
func (t *transaction) PutRequest(ctx context.Context, v record.AcceptedRequest) error {
	if v.ID == "" || v.AcceptedAt.IsZero() || len(v.Payload) == 0 {
		return state.ErrInvalid
	}
	if old, err := t.Request(ctx, v.ID); err == nil {
		if old.CompletedAt != nil && !reflect.DeepEqual(old.CompletedAt, v.CompletedAt) {
			return state.ErrConflict
		}
		old.CompletedAt = v.CompletedAt
		if err = immutable(old, v); err != nil {
			return err
		}
		return t.exec(ctx, "UPDATE requests SET completed_at=? WHERE repository_id=? AND id=?", nullableTime(v.CompletedAt), t.repo, v.ID)
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	return t.exec(ctx, "INSERT INTO requests(id,repository_id,kind,payload,accepted_at,completed_at) VALUES(?,?,?,?,?,?)", v.ID, t.repo, v.Kind, v.Payload, v.AcceptedAt.UnixMilli(), nullableTime(v.CompletedAt))
}

type jobOptions struct {
	SourceBranch      string                         `json:",omitempty"`
	Publication       *record.PublicationSpec        `json:",omitempty"`
	PublishTo         *record.PublicationDestination `json:",omitempty"`
	FreshVerification bool                           `json:",omitempty"`
	Targets           []record.Target
	Build             *record.BuildConfig
	BuildRequirements *record.BuildRequirements `json:",omitempty"`
	Version, Reason   string
	Preparation       *record.PreparationSpec
	Checkout          *record.Checkout `json:",omitempty"`
}

func (t *transaction) Job(ctx context.Context, id record.JobID) (record.Job, error) {
	var v record.Job
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var change, specChange, input, result sql.NullString
	var source, raw string
	var accepted int64
	var canceled, admitted, finished, until, retry sql.NullInt64
	var owner, prepared, release, reused sql.NullString
	err := t.conn.QueryRowContext(ctx, `SELECT id,request_id,change_id,spec_change_id,input_revision,result_revision,source_id,action,phase,destination,verification,options,state,accepted_at,cancel_at,admitted_at,finished_at,detail,claim_owner,claim_generation,claim_until,retry_at,prepared,resolved_release,reused_attempt,reuse_detail FROM jobs WHERE repository_id=? AND id=?`, t.repo, id).Scan(&v.ID, &v.RequestID, &change, &specChange, &input, &result, &source, &v.Spec.Action, &v.Phase, &v.Spec.Destination, &v.Spec.Verification, &raw, &v.State, &accepted, &canceled, &admitted, &finished, &v.Detail, &owner, &v.ClaimGeneration, &until, &retry, &prepared, &release, &reused, &v.ReuseDetail)
	if err != nil {
		return v, storageError(err)
	}
	var options jobOptions
	if err = decode(raw, &options); err != nil {
		return v, err
	}
	v.Spec.Targets, v.Spec.Build, v.Spec.BuildRequirements, v.Spec.Version, v.Spec.Reason = options.Targets, options.Build, options.BuildRequirements, options.Version, options.Reason
	v.Spec.SourceBranch = options.SourceBranch
	v.Spec.Publication = options.Publication
	v.Spec.PublishTo = options.PublishTo
	v.Spec.Preparation = options.Preparation
	v.Spec.Checkout = options.Checkout
	v.Spec.FreshVerification = options.FreshVerification
	v.ReusedAttempt = record.AttemptID(reused.String)
	v.Claim = readClaim(owner, v.ClaimGeneration, until)
	v.RetryAt = scanTime(retry)
	if release.Valid {
		v.ResolvedRelease = &record.Release{}
		if err = decode(release.String, v.ResolvedRelease); err != nil {
			return v, err
		}
	}
	if prepared.Valid {
		v.Prepared = &record.PreparedChange{}
		if err = decode(prepared.String, v.Prepared); err != nil {
			return v, err
		}
	}
	v.ChangeID = record.ChangeID(change.String)
	v.Spec.ChangeID = record.ChangeID(specChange.String)
	v.Spec.InputRevision = record.RevisionID(input.String)
	v.ResultRevision = record.RevisionID(result.String)
	v.AcceptedAt = fromTime(accepted)
	v.CancelRequestedAt = scanTime(canceled)
	v.AdmittedAt = scanTime(admitted)
	v.FinishedAt = scanTime(finished)
	v.Spec.Source, err = t.readSource(ctx, source)
	return v, err
}
func (t *transaction) JobForRequest(ctx context.Context, id record.RequestID) (record.Job, error) {
	if err := t.check(ctx, false); err != nil {
		return record.Job{}, err
	}
	var job record.JobID
	if err := t.conn.QueryRowContext(ctx, "SELECT id FROM jobs WHERE repository_id=? AND request_id=?", t.repo, id).Scan(&job); err != nil {
		return record.Job{}, storageError(err)
	}
	return t.Job(ctx, job)
}
func (t *transaction) PutJob(ctx context.Context, v record.Job) error {
	if v.ID == "" || v.RequestID == "" || v.AcceptedAt.IsZero() || jobPhaseOrder(v.Phase) == 0 {
		return state.ErrInvalid
	}
	if v.ReusedAttempt != "" {
		if _, err := t.Attempt(ctx, v.ReusedAttempt); err != nil {
			return err
		}
	}
	owner, until, err := claimValues(v.Claim, v.ClaimGeneration)
	if err != nil {
		return err
	}
	var release any
	if r := v.ResolvedRelease; r != nil {
		if v.Spec.Action != record.Bump || r.Requested != v.Spec.Version || r.Version == "" || r.Forge == "" || r.Instance == "" || r.Repository == "" || r.Tag == "" || !objectID(record.ObjectID(r.Commit)) || r.ObservedAt.IsZero() {
			return state.ErrInvalid
		}
		if r.Requested == "" && r.CurrentVersion == "" {
			return state.ErrInvalid
		}
		if r.NoUpdate && (v.Spec.Version != "" || v.State != record.JobCompleted || v.Prepared != nil || v.ResultRevision != "") {
			return state.ErrInvalid
		}
		release, err = encode(r)
		if err != nil {
			return err
		}
	}
	var prepared any
	if v.Prepared != nil {
		if v.Prepared.Branch == "" || !objectID(v.Prepared.Source.Commit) || !objectID(v.Prepared.Source.Tree) {
			return state.ErrInvalid
		}
		prepared, err = encode(v.Prepared)
		if err != nil {
			return err
		}
	}
	old, err := t.Job(ctx, v.ID)
	if err == nil {
		if err = immutable(old.Spec, v.Spec); err != nil {
			return err
		}
		if old.RequestID != v.RequestID || !old.AcceptedAt.Equal(v.AcceptedAt) || old.ClaimGeneration > v.ClaimGeneration {
			return state.ErrConflict
		}
		if old.Phase != v.Phase {
			if jobPhaseOrder(v.Phase) != jobPhaseOrder(old.Phase)+1 || old.State != record.JobActive || v.State != record.JobActive || old.FinishedAt != nil || v.FinishedAt != nil {
				return state.ErrConflict
			}
		}
		if old.ReusedAttempt != "" && old.ReusedAttempt != v.ReusedAttempt {
			return state.ErrConflict
		}
		if old.ResolvedRelease != nil {
			if err = immutable(old.ResolvedRelease, v.ResolvedRelease); err != nil {
				return err
			}
		}
		if old.Prepared != nil {
			if v.Prepared == nil || old.Prepared.Branch != v.Prepared.Branch || old.Prepared.Source != v.Prepared.Source || (old.Prepared.IntegrationStarted && !v.Prepared.IntegrationStarted) {
				return state.ErrConflict
			}
		}
		if old.ResultRevision != "" && (v.ResultRevision != old.ResultRevision || v.ChangeID != old.ChangeID) {
			return state.ErrConflict
		}
		if err = t.exec(ctx, "UPDATE jobs SET change_id=?,result_revision=?,phase=?,state=?,cancel_at=?,admitted_at=?,finished_at=?,detail=?,claim_owner=?,claim_generation=?,claim_until=?,retry_at=?,prepared=?,resolved_release=?,reused_attempt=?,reuse_detail=? WHERE repository_id=? AND id=?", nullableID(v.ChangeID), nullableID(v.ResultRevision), v.Phase, v.State, nullableTime(v.CancelRequestedAt), nullableTime(v.AdmittedAt), nullableTime(v.FinishedAt), v.Detail, owner, v.ClaimGeneration, until, nullableTime(v.RetryAt), prepared, release, nullableID(v.ReusedAttempt), v.ReuseDetail, t.repo, v.ID); err != nil {
			return err
		}
		return t.scheduleJob(ctx, v.ID)
	}
	if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	request, err := t.Request(ctx, v.RequestID)
	if err != nil {
		return err
	}
	if request.Kind != record.JobRequest {
		return state.ErrConflict
	}
	if v.Spec.InputRevision != "" {
		revision, e := t.Revision(ctx, v.Spec.InputRevision)
		if e != nil {
			return e
		}
		if revision.ChangeID != v.Spec.ChangeID || revision.Source != v.Spec.Source {
			return state.ErrConflict
		}
	}
	source, err := t.source(ctx, v.Spec.Source)
	if err != nil {
		return err
	}
	raw, err := encode(jobOptions{SourceBranch: v.Spec.SourceBranch, Publication: v.Spec.Publication, PublishTo: v.Spec.PublishTo, Targets: v.Spec.Targets, Build: v.Spec.Build, BuildRequirements: v.Spec.BuildRequirements, Version: v.Spec.Version, Reason: v.Spec.Reason, Preparation: v.Spec.Preparation, Checkout: v.Spec.Checkout, FreshVerification: v.Spec.FreshVerification})
	if err != nil {
		return err
	}
	err = t.exec(ctx, `INSERT INTO jobs(id,repository_id,request_id,change_id,spec_change_id,input_revision,result_revision,source_id,action,phase,destination,verification,options,state,accepted_at,cancel_at,admitted_at,finished_at,detail,claim_owner,claim_generation,claim_until,retry_at,prepared,resolved_release,reused_attempt,reuse_detail) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, t.repo, v.RequestID, nullableID(v.ChangeID), nullableID(v.Spec.ChangeID), nullableID(v.Spec.InputRevision), nullableID(v.ResultRevision), source, v.Spec.Action, v.Phase, v.Spec.Destination, v.Spec.Verification, raw, v.State, v.AcceptedAt.UnixMilli(), nullableTime(v.CancelRequestedAt), nullableTime(v.AdmittedAt), nullableTime(v.FinishedAt), v.Detail, owner, v.ClaimGeneration, until, nullableTime(v.RetryAt), prepared, release, nullableID(v.ReusedAttempt), v.ReuseDetail)
	if err != nil {
		return err
	}
	return t.scheduleJob(ctx, v.ID)
}

func jobPhaseOrder(phase record.JobPhase) int {
	switch phase {
	case record.PhasePreparation:
		return 1
	case record.PhaseVerification:
		return 2
	case record.PhasePublication:
		return 3
	default:
		return 0
	}
}
func (t *transaction) scheduleJob(ctx context.Context, id record.JobID) error {
	return t.exec(ctx, `UPDATE jobs SET next_action_at=CASE WHEN state NOT IN ('queued','active') THEN NULL
 WHEN action IN ('bump','bump-revision') AND cancel_at IS NOT NULL AND prepared IS NULL THEN 0
 ELSE max(coalesce(claim_until,0),coalesce(retry_at,0),coalesce((SELECT min(CASE WHEN jobs.cancel_at IS NOT NULL AND a.state='queued' THEN coalesce(a.claim_until,0) ELSE a.next_action_at END) FROM attempts a WHERE a.repository_id=jobs.repository_id AND a.job_id=jobs.id),0)) END WHERE repository_id=? AND id=?`, t.repo, id)
}
func (t *transaction) Plan(ctx context.Context, id record.JobID) (record.VerificationPlan, error) {
	var v record.VerificationPlan
	if err := t.check(ctx, false); err != nil {
		return v, err
	}
	var raw string
	err := t.conn.QueryRowContext(ctx, "SELECT job_id,coalesce(revision_id,''),targets FROM plans WHERE repository_id=? AND job_id=?", t.repo, id).Scan(&v.JobID, &v.RevisionID, &raw)
	if err != nil {
		return v, storageError(err)
	}
	return v, decode(raw, &v.Targets)
}
func (t *transaction) PutPlan(ctx context.Context, v record.VerificationPlan) error {
	if v.JobID == "" || len(v.Targets) == 0 {
		return state.ErrInvalid
	}
	if old, err := t.Plan(ctx, v.JobID); err == nil {
		return immutable(old, v)
	} else if !errors.Is(err, state.ErrNotFound) {
		return err
	}
	job, err := t.Job(ctx, v.JobID)
	if err != nil {
		return err
	}
	expected := job.ResultRevision
	if expected == "" {
		expected = job.Spec.InputRevision
	}
	if v.RevisionID != expected {
		return state.ErrConflict
	}
	raw, err := encode(v.Targets)
	if err != nil {
		return err
	}
	return t.exec(ctx, "INSERT INTO plans(repository_id,job_id,revision_id,targets) VALUES(?,?,?,?)", t.repo, v.JobID, nullableID(v.RevisionID), raw)
}

func claimValues(claim *record.Claim, generation uint64) (any, any, error) {
	if generation > 1<<63-1 {
		return nil, nil, state.ErrInvalid
	}
	if claim == nil {
		return nil, nil, nil
	}
	if claim.Owner == "" || claim.Generation != generation || generation == 0 || claim.ExpiresAt.IsZero() {
		return nil, nil, state.ErrInvalid
	}
	return string(claim.Owner), claim.ExpiresAt.UnixMilli(), nil
}
func readClaim(owner sql.NullString, generation uint64, until sql.NullInt64) *record.Claim {
	if !owner.Valid {
		return nil
	}
	return &record.Claim{Owner: record.ProcessID(owner.String), Generation: generation, ExpiresAt: fromTime(until.Int64)}
}
func nextTime(claim *record.Claim, retry *time.Time) int64 {
	next := int64(0)
	if retry != nil {
		next = retry.UnixMilli()
	}
	if claim != nil {
		next = max(next, claim.ExpiresAt.UnixMilli())
	}
	return next
}

func (t *transaction) OpenChangeByBranch(ctx context.Context, branch string) (record.Change, error) {
	if err := t.check(ctx, false); err != nil {
		return record.Change{}, err
	}
	if branch == "" {
		return record.Change{}, state.ErrInvalid
	}
	var id record.ChangeID
	err := t.conn.QueryRowContext(ctx, "SELECT id FROM changes WHERE repository_id=? AND branch=? AND disposition='open'", t.repo, branch).Scan(&id)
	if err != nil {
		return record.Change{}, storageError(err)
	}
	return t.Change(ctx, id)
}
