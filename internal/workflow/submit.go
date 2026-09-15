package workflow

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// Request supplies a caller-owned identity and the intent to accept.
// Preserve the original request for retries, including after an uncertain commit.
// A stored [record.JobSpec] may contain source fields filled by intake and is not a
// substitute for the original request when retrying.
type Request struct {
	ID   record.RequestID
	Spec record.JobSpec
	// Branch adopts the explicit source as a revision of an existing change.
	// Publish also creates the contribution when the branch is not yet tracked.
	Branch *BranchInput
}

// Receipt confirms durable acceptance of one job. It does not establish driver
// pickup, provider admission, or completion. Equivalent retries return the same
// job identity and acceptance time.
type Receipt struct {
	RequestID  record.RequestID
	JobID      record.JobID
	AcceptedAt time.Time
}

// Submit validates and durably accepts a request in one state transaction.
// A Branch input adopts its frozen source as a revision in that transaction.
// It copies and normalizes the requested inputs, binds an existing revision when
// selected, and records a queued job. It performs no preparation or provider work.
//
// Reusing an ID with equivalent intent returns the original receipt, even after
// the job progresses or its change advances or closes. Different intent returns
// an error wrapping ErrRequestConflict. Every error returns a zero receipt,
// including a state commit whose outcome is uncertain; retry with the same ID
// rather than creating a new request.
func (e *Engine) Submit(ctx context.Context, request Request) (Receipt, error) {
	if e == nil || e.State == nil || e.Repository == "" {
		return Receipt{}, ErrNoState
	}
	if !validToken(string(request.ID)) {
		return Receipt{}, fmt.Errorf("%w: a nonempty request ID without whitespace or control characters is required", ErrInvalidRequest)
	}
	spec, err := normalizeSpec(request.Spec)
	if err != nil {
		return Receipt{}, err
	}
	if request.Branch != nil {
		branch := *request.Branch
		if branch.InferredTarget != nil {
			target := *branch.InferredTarget
			target.Variants = maps.Clone(target.Variants)
			branch.InferredTarget = &target
		}
		request.Branch = &branch
	}
	if err := validateBranchInput(request.Branch, spec); err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	intent := spec
	if intent.InputRevision != "" {
		intent.ChangeID = ""
	}
	var payload []byte
	if request.Branch == nil {
		payload, err = json.Marshal(intent)
	} else {
		payload, err = json.Marshal(struct {
			Spec   record.JobSpec
			Branch BranchInput
		}{intent, *request.Branch})
	}
	if err != nil {
		return Receipt{}, err
	}
	err = e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		previous, err := tx.Request(ctx, request.ID)
		if err == nil {
			if previous.Kind != record.JobRequest || !bytes.Equal(previous.Payload, payload) {
				return ErrRequestConflict
			}
			job, err := tx.JobForRequest(ctx, request.ID)
			if err != nil {
				return err
			}
			if spec.ChangeID != "" && spec.ChangeID != job.Spec.ChangeID {
				return ErrRequestConflict
			}
			receipt = Receipt{RequestID: request.ID, JobID: job.ID, AcceptedAt: job.AcceptedAt}
			return nil
		}
		if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		now := e.now()
		if now.IsZero() {
			return ErrInvalidRequest
		}
		var accepted record.JobSpec
		if request.Branch == nil {
			accepted, err = bindRevision(ctx, spec, tx)
		} else {
			accepted, err = adoptBranch(ctx, tx, spec, *request.Branch, now)
		}
		if err != nil {
			return err
		}
		if accepted.Preparation != nil && accepted.Preparation.Correction != nil {
			correction := *accepted.Preparation.Correction
			if _, err := correctionCurrent(ctx, tx, correction); err != nil {
				return err
			}
			if err := correctionIdle(ctx, tx, correction.ChangeID, ""); err != nil {
				return err
			}
			accepted.ChangeID = correction.ChangeID
		}
		id := record.JobID("job_" + rand.Text())
		if err = tx.PutRequest(ctx, record.AcceptedRequest{ID: request.ID, Kind: record.JobRequest, Payload: payload, AcceptedAt: now}); err != nil {
			return err
		}
		if err = tx.PutJob(ctx, record.Job{ID: id, RequestID: request.ID, Spec: accepted, ChangeID: accepted.ChangeID, State: record.JobQueued, Phase: initialPhase(accepted.Action), AcceptedAt: now}); err != nil {
			return err
		}
		if accepted.Action == record.Publish {
			job, err := tx.Job(ctx, id)
			if err != nil {
				return err
			}
			if accepted.ChangeID == "" || accepted.Source.Commit != accepted.Publication.Desired.Head {
				return ErrInvalidRequest
			}
			change, err := tx.Change(ctx, accepted.ChangeID)
			if err != nil {
				return err
			}
			if change.Branch != accepted.Publication.SourceBranch() {
				return ErrInvalidRequest
			}
			if err := publicationEvidence(ctx, tx, job, *job.Spec.Publication); err != nil {
				return err
			}
			action := record.PublicationAction{ID: record.PublicationID("publication_" + rand.Text()), JobID: id, ChangeID: accepted.ChangeID, RevisionID: accepted.InputRevision, Spec: *accepted.Publication, State: record.PublicationPending}
			if err := validatePublicationAction(job, action); err != nil {
				return err
			}
			if err := tx.PutPublication(ctx, action); err != nil {
				return fmt.Errorf("accept publication (another job may own this remote branch): %w", err)
			}
		}
		receipt = Receipt{RequestID: request.ID, JobID: id, AcceptedAt: now}
		return nil
	})
	if err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

// bindRevision resolves an existing revision from a transaction's snapshot and
// freezes its source into a new job specification. Verification may select an
// older revision of an open change; other actions require its current revision.
// Requests with an explicit source and no input revision pass through unchanged.
func bindRevision(ctx context.Context, spec record.JobSpec, reader state.Reader) (record.JobSpec, error) {
	if spec.InputRevision == "" {
		return spec, nil
	}
	revision, err := reader.Revision(ctx, spec.InputRevision)
	if err != nil {
		return record.JobSpec{}, err
	}
	change, err := reader.Change(ctx, revision.ChangeID)
	if err != nil {
		return record.JobSpec{}, err
	}

	if spec.ChangeID != "" && spec.ChangeID != change.ID {
		return record.JobSpec{}, fmt.Errorf("%w: revision %s does not belong to change %s", ErrInvalidRequest, revision.ID, spec.ChangeID)
	}
	if change.Disposition != record.ChangeOpen {
		return record.JobSpec{}, fmt.Errorf("%w: change %s is %s", ErrInvalidRequest, change.ID, change.Disposition)
	}
	if spec.Action != record.Verify && change.CurrentRevision != revision.ID {
		return record.JobSpec{}, fmt.Errorf("%w: revision %s is no longer current for change %s", ErrStaleRevision, revision.ID, change.ID)
	}
	if err := validateSource(revision.Source); err != nil {
		return record.JobSpec{}, fmt.Errorf("%w: revision %s: %v", state.ErrInvalid, revision.ID, err)
	}
	if spec.Action == record.Publish && revision.Source.Commit == "" {
		return record.JobSpec{}, fmt.Errorf("%w: publication requires a committed revision", ErrInvalidRequest)
	}
	if err := correctionNotPending(ctx, reader, change.ID); err != nil {
		return record.JobSpec{}, err
	}
	spec.ChangeID = change.ID
	spec.Source = revision.Source
	return spec, nil
}
