package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
)

// Control durably records an idempotent cancellation request for explicit jobs.
// Callers supply an ID, Cancel kind, job IDs, and optional reason; change/revision
// selectors and timestamps must be unset. Other control kinds remain unsupported.
//
// Repeated job IDs and their order do not change intent. An equivalent retry is
// a no-op; reuse of an ID for other intent returns ErrRequestConflict. A successful
// call records intent only. A later Cycle applies it and reconciles any remote
// cancellation. After an uncertain state commit, retry the original request.
func (e *Engine) Control(ctx context.Context, request record.ControlRequest) error {
	if e == nil || e.State == nil || e.Repository == "" {
		return ErrNoState
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
	return e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		accepted, err := tx.Request(ctx, request.ID)
		if err == nil {
			if accepted.Kind != record.CancelRequest {
				return ErrRequestConflict
			}
			previous, err := tx.Control(ctx, request.ID)
			if err != nil {
				return err
			}
			previous.SubmittedAt = request.SubmittedAt
			previous.AppliedAt = nil
			if !reflect.DeepEqual(previous, request) {
				return ErrRequestConflict
			}
			return nil
		}
		if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		for _, id := range request.Jobs {
			if _, err = tx.Job(ctx, id); err != nil {
				return err
			}
		}
		request.SubmittedAt = e.now()
		return tx.PutControl(ctx, request)
	})
}

func (e *Engine) applyControls(ctx context.Context, scope Scope, controls []record.ControlRequest) error {
	selected := map[record.JobID]bool{}
	for _, id := range scope.Jobs {
		selected[id] = true
	}
	for _, candidate := range controls {
		err := e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
			current, err := tx.Control(ctx, candidate.ID)
			if err != nil {
				return err
			}
			if current.AppliedAt != nil {
				return nil
			}
			now := e.now()
			for _, id := range current.Jobs {
				job, err := tx.Job(ctx, id)
				if err != nil {
					return err
				}
				applicable := scope.All || selected[id]
				if job.CancelRequestedAt == nil && !jobTerminal(job.State) {
					if !applicable {
						continue
					}
					job.CancelRequestedAt = &now
					if err = tx.PutJob(ctx, job); err != nil {
						return err
					}
					attempts, err := tx.AttemptsForJob(ctx, id)
					if err != nil {
						return err
					}
					for _, attempt := range attempts {
						if !attemptTerminal(attempt.State) && attempt.RetryAt != nil {
							attempt.RetryAt = nil
							if err := tx.PutAttempt(ctx, attempt); err != nil {
								return err
							}
						}
					}
				}
				if err = tx.ApplyControl(ctx, current.ID, id, now); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
