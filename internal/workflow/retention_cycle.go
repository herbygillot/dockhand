package workflow

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

const diagnosticRetention = 7 * 24 * time.Hour

// pruneDiagnostics performs a bounded maintenance batch for this repository.
// Retained VMs and exported build outputs are never selected. Provider locks
// serialize idempotent deletion; released ownership cannot change underneath it.
func (c *cycle) pruneDiagnostics(ctx context.Context, result *CycleResult) error {
	e := c.engine
	now := e.now()
	before := now.Add(-diagnosticRetention)
	var resources []record.Resource
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		resources, err = r.Resources(ctx, state.Query{PruneBefore: &before, DueBefore: &now, Limit: 8})
		return err
	})
	if err != nil {
		return err
	}
	for _, resource := range resources {
		item, err := c.collectResource(ctx, resource.ID, before, false)
		if err != nil {
			return err
		}
		if item.Completed {
			continue
		}
		// Skip busy, unsupported, or failed candidates until another day so one old
		// resource cannot starve the bounded batch on every cycle.
		err = e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
			current, err := tx.Resource(ctx, resource.ID)
			if err != nil {
				return err
			}
			if current.State != record.ResourceReleased || current.Handle != resource.Handle {
				return state.ErrConflict
			}
			if current.ArtifactsPrunedAt != nil {
				return nil
			}
			retry := e.now().Add(24 * time.Hour)
			current.RetryAt, current.LastError = &retry, item.Detail
			return tx.PutResource(ctx, current)
		})
		if err != nil {
			return err
		}
		if item.Detail != "" {
			result.Problems = append(result.Problems, JobProblem{ResourceID: resource.ID, Detail: item.Detail})
		}
	}
	return nil
}
