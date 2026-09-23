package retention

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

// Action names what a terminal resource is due for at the threshold:
// "release" for a retained or unsettled environment, "prune-artifacts" for
// a released one whose diagnostics are old enough, and nothing otherwise.
func Action(r record.Resource, a record.Attempt, j record.Job, before, now time.Time) string {
	if !a.State.Terminal() || !j.State.Terminal() || j.FinishedAt == nil || j.FinishedAt.After(before) || r.Claim.Live(now) || a.Claim.Live(now) || j.Claim.Live(now) {
		return ""
	}
	if r.Handle.Provider != a.Spec.Config.Provider || !validToken(r.Handle.ID) {
		return ""
	}
	switch r.State {
	case record.ResourceReleased:
		if r.ReleasedAt != nil && !r.ReleasedAt.After(before) && r.ArtifactsPrunedAt == nil && (a.Evidence == nil || len(a.Evidence.Artifacts) == 0) {
			return "prune-artifacts"
		}
	case record.ResourceRetained:
		if r.RetainUntil != nil && r.RetainUntil.After(now) {
			return ""
		}
		if due(r.RetryAt, now) {
			return "release"
		}
	case record.ResourceReleaseRequested, record.ResourceUncertain:
		if due(r.RetryAt, now) {
			return "release"
		}
	}
	return ""
}

// CollectResource takes the action one resource is due for, or previews it.
func (c *Collector) CollectResource(ctx context.Context, id record.ResourceID, before time.Time, dry bool) (Item, error) {
	item := Item{ResourceID: id}
	var resource record.Resource
	read := func(ctx context.Context, r state.Reader) error {
		var err error
		resource, err = r.Resource(ctx, id)
		if err != nil {
			return err
		}
		attempt, err := r.Attempt(ctx, resource.AttemptID)
		if err != nil {
			return err
		}
		job, err := r.Job(ctx, attempt.JobID)
		if err != nil {
			return err
		}
		item.Action = Action(resource, attempt, job, before, c.Now())
		return nil
	}
	if err := c.State.View(ctx, read); err != nil {
		return item, err
	}
	if dry || item.Action == "" {
		return item, nil
	}
	if item.Action == "release" {
		// Persist expiry before calling the engine's claimed, recoverable
		// release path.
		err := c.State.Update(ctx, func(ctx context.Context, tx state.Tx) error {
			if err := read(ctx, tx); err != nil {
				return err
			}
			if item.Action != "release" {
				return nil
			}
			if resource.State == record.ResourceRetained {
				now := c.Now()
				resource.RetainUntil = &now
				return tx.PutResource(ctx, resource)
			}
			return nil
		})
		if err != nil || item.Action != "release" {
			return item, err
		}
		item.Detail, err = c.Release(ctx, id, resource.Handle.Provider)
		if err != nil {
			return item, err
		}
		err = c.State.View(ctx, func(ctx context.Context, r state.Reader) error {
			v, err := r.Resource(ctx, id)
			item.Completed = err == nil && v.State == record.ResourceReleased
			return err
		})
		if !item.Completed && item.Detail == "" {
			item.Detail = "release remains pending; another driver may own cleanup"
		}
		return item, err
	}
	pruner, ok := c.Provider(resource.Handle.Provider).(verify.ArtifactPruner)
	if !ok {
		item.Detail = "provider does not support artifact pruning"
		return item, nil
	}
	// Released ownership is immutable. The provider serializes idempotent
	// removal; no expiring workflow lease is needed for these already
	// released files.
	callCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	err := pruner.PruneArtifacts(callCtx, resource.Handle)
	if err == nil {
		err = callCtx.Err()
	}
	cancel()
	if err != nil {
		item.Detail = err.Error()
		return item, nil
	}
	err = c.State.Update(ctx, func(ctx context.Context, tx state.Tx) error {
		current, err := tx.Resource(ctx, id)
		if err != nil {
			return err
		}
		if current.State != record.ResourceReleased || current.Handle != resource.Handle {
			return state.ErrConflict
		}
		if current.ArtifactsPrunedAt == nil {
			now := c.Now()
			current.ArtifactsPrunedAt = &now
			current.RetryAt, current.LastError = nil, ""
			if err = tx.PutResource(ctx, current); err != nil {
				return err
			}
		}
		item.Completed = true
		return nil
	})
	return item, err
}

func due(at *time.Time, now time.Time) bool { return at == nil || !at.After(now) }
