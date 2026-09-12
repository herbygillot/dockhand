package workflow

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

func (e *Engine) Control(ctx context.Context, request record.ControlRequest) error {
	if e == nil || e.Ledger == nil {
		return ErrNoLedger
	}
	if request.Kind != record.Cancel {
		return fmt.Errorf("%w: control %s", ErrUnsupportedAction, request.Kind)
	}
	if !validToken(string(request.ID)) || len(request.Jobs) == 0 || request.ChangeID != "" || request.ExpectedRevision != "" || !request.SubmittedAt.IsZero() || request.AppliedAt != nil || !utf8.ValidString(request.Reason) {
		return fmt.Errorf("%w: cancellation requires a request ID and explicit job IDs; timestamps are driver-owned", ErrInvalidRequest)
	}
	request.Jobs = slices.Clone(request.Jobs)
	slices.Sort(request.Jobs)
	request.Jobs = slices.Compact(request.Jobs)
	for _, id := range request.Jobs {
		if !validToken(string(id)) {
			return fmt.Errorf("%w: invalid job ID", ErrInvalidRequest)
		}
	}
	return e.Ledger.Update(ctx, func(_ context.Context, tx *ledger.Transaction) error {
		if _, exists := tx.State.Requests[request.ID]; exists {
			return ErrRequestConflict
		}
		if previous, exists := tx.State.Controls[request.ID]; exists {
			previous.SubmittedAt = request.SubmittedAt
			previous.AppliedAt = nil
			if !reflect.DeepEqual(previous, request) {
				return ErrRequestConflict
			}
			return nil
		}
		for _, id := range request.Jobs {
			if _, exists := tx.State.Jobs[id]; !exists {
				return fmt.Errorf("%w: job %s", ErrNotFound, id)
			}
		}
		request.SubmittedAt = e.now()
		if request.SubmittedAt.IsZero() {
			return fmt.Errorf("workflow: control clock returned a zero time")
		}
		tx.State.Controls[request.ID] = request
		return nil
	})
}

func (e *Engine) applyControls(ctx context.Context, selected map[record.JobID]bool) error {
	return e.Ledger.Update(ctx, func(_ context.Context, tx *ledger.Transaction) error {
		now := e.now()
		for id, request := range tx.State.Controls {
			if request.Kind != record.Cancel || request.AppliedAt != nil {
				continue
			}
			applied := true
			for _, jobID := range request.Jobs {
				job, exists := tx.State.Jobs[jobID]
				if !exists {
					return fmt.Errorf("%w: control %s names missing job %s", ledger.ErrInvalidState, id, jobID)
				}
				if job.CancelRequestedAt == nil && !jobTerminal(job.State) {
					if selected[jobID] {
						job.CancelRequestedAt = &now
						tx.State.Jobs[jobID] = job
					} else {
						applied = false
					}
				}
			}
			if applied {
				request.AppliedAt = &now
				tx.State.Controls[id] = request
			}
		}
		return nil
	})
}
