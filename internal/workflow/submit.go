package workflow

import (
	"context"
	"crypto/rand"
	"fmt"
	"reflect"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

// Request supplies a caller-owned identity and the intent to accept.
// Preserve the original request for retries, including after an uncertain commit.
// A stored [record.JobSpec] may contain source fields filled by intake and is not a
// substitute for the original request when retrying.
type Request struct {
	ID   record.RequestID
	Spec record.JobSpec
}

// Receipt confirms durable acceptance of one job. It does not establish driver
// pickup, provider admission, or completion. Equivalent retries return the same
// job identity and acceptance time.
type Receipt struct {
	RequestID  record.RequestID
	JobID      record.JobID
	AcceptedAt time.Time
}

// Submit validates and durably accepts a request in one ledger transaction.
// It copies and normalizes the requested inputs, binds an existing revision when
// selected, and records a queued job. It performs no preparation or provider work.
//
// Reusing an ID with equivalent intent returns the original receipt, even after
// the job progresses or its change advances or closes. Different intent returns
// an error wrapping ErrRequestConflict. Every error returns a zero receipt,
// including a ledger commit whose outcome is uncertain; retry with the same ID
// rather than creating a new request.
func (e *Engine) Submit(ctx context.Context, request Request) (Receipt, error) {
	if e == nil || e.Ledger == nil {
		return Receipt{}, ErrNoLedger
	}
	if !validToken(string(request.ID)) {
		return Receipt{}, fmt.Errorf("%w: a nonempty request ID without whitespace or control characters is required", ErrInvalidRequest)
	}
	spec, err := normalizeSpec(request.Spec)
	if err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	err = e.Ledger.Update(ctx, func(_ context.Context, tx *ledger.Transaction) error {
		if _, exists := tx.State.Controls[request.ID]; exists {
			return fmt.Errorf("%w: %s identifies a control request", ErrRequestConflict, request.ID)
		}
		// Compare against accepted intent before checking the change's present
		// state, so later progress cannot invalidate an equivalent retry.
		if id, exists := tx.State.Requests[request.ID]; exists {
			job := tx.State.Jobs[id]
			candidate := spec
			if candidate.InputRevision != "" {
				candidate.Source = job.Spec.Source
				if candidate.ChangeID == "" {
					candidate.ChangeID = job.Spec.ChangeID
				}
			}
			if !reflect.DeepEqual(candidate, job.Spec) {
				return fmt.Errorf("%w: %s was accepted with different intent", ErrRequestConflict, request.ID)
			}
			receipt = Receipt{RequestID: request.ID, JobID: job.ID, AcceptedAt: job.AcceptedAt}
			return nil
		}
		accepted, err := bindRevision(spec, tx.State)
		if err != nil {
			return err
		}
		id := record.JobID("job_" + rand.Text())
		if _, exists := tx.State.Jobs[id]; exists {
			return fmt.Errorf("workflow: generated job ID already exists: %s", id)
		}
		now := e.now()
		if now.IsZero() {
			return fmt.Errorf("workflow: acceptance clock returned a zero time")
		}
		tx.State.Jobs[id] = record.Job{
			ID: id, RequestID: request.ID, Spec: accepted,
			ChangeID: accepted.ChangeID, State: record.JobQueued, AcceptedAt: now,
		}
		tx.State.Requests[request.ID] = id
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
func bindRevision(spec record.JobSpec, state ledger.State) (record.JobSpec, error) {
	if spec.InputRevision == "" {
		return spec, nil
	}
	revision, exists := state.Revisions[spec.InputRevision]
	if !exists {
		return record.JobSpec{}, fmt.Errorf("%w: revision %s", ErrNotFound, spec.InputRevision)
	}
	change, exists := state.Changes[revision.ChangeID]
	if !exists {
		return record.JobSpec{}, fmt.Errorf("%w: revision %s has no recorded change", ledger.ErrInvalidState, revision.ID)
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
		return record.JobSpec{}, fmt.Errorf("%w: revision %s: %v", ledger.ErrInvalidState, revision.ID, err)
	}
	if spec.Action == record.Publish && revision.Source.Commit == "" {
		return record.JobSpec{}, fmt.Errorf("%w: publication requires a committed revision", ErrInvalidRequest)
	}
	spec.ChangeID = change.ID
	spec.Source = revision.Source
	return spec, nil
}
