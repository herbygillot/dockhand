package workflow

import (
	"context"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

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
				if job.CancelRequestedAt == nil && !job.State.Terminal() {
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
						if !attempt.State.Terminal() && attempt.RetryAt != nil {
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
