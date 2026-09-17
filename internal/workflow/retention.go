package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

type RetentionOptions struct {
	OlderThan time.Duration
	DryRun    bool
}

type CleanupItem struct {
	// Repository is set by callers that collect several registrations.
	Repository record.RepositoryID `json:",omitempty"`
	ResourceID record.ResourceID
	AttemptID  record.AttemptID
	Path       string
	Action     string
	Completed  bool
	Detail     string
}

// RetentionResult can describe partial progress when collection returns an error.
type RetentionResult struct {
	Before time.Time
	DryRun bool
	Items  []CleanupItem
}

// Collect releases old terminal resources through the driver's existing cleanup
// path and prunes older released diagnostics. It never advances jobs or forgets
// their identities. Explicit collection also releases retained failed environments.
func (e *Engine) Collect(ctx context.Context, options RetentionOptions) (RetentionResult, error) {
	result := RetentionResult{DryRun: options.DryRun, Items: []CleanupItem{}}
	if err := e.checkScope(Scope{All: true}); err != nil {
		return result, err
	}
	if options.OlderThan < 0 {
		return result, fmt.Errorf("workflow: retention age must not be negative")
	}
	c, err := e.newCycle()
	if err != nil {
		return result, err
	}
	result.Before = e.now().Add(-options.OlderThan)
	q := state.Query{Limit: 64, CleanupBefore: &result.Before}
	for {
		var resources []record.Resource
		err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
			var err error
			resources, err = r.Resources(ctx, q)
			return err
		})
		if err != nil {
			return result, err
		}
		for _, resource := range resources {
			item, err := c.collectResource(ctx, resource.ID, result.Before, options.DryRun)
			if item.Action != "" {
				result.Items = append(result.Items, item)
			}
			if err != nil {
				return result, err
			}
		}
		if len(resources) < q.Limit {
			err := c.collectLogCaches(ctx, &result)
			return result, err
		}
		q.After = string(resources[len(resources)-1].ID)
	}
}

func retentionAction(r record.Resource, a record.Attempt, j record.Job, before, now time.Time) string {
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

func (c *cycle) collectResource(ctx context.Context, id record.ResourceID, before time.Time, dry bool) (CleanupItem, error) {
	e := c.engine
	item := CleanupItem{ResourceID: id}
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
		item.Action = retentionAction(resource, attempt, job, before, e.now())
		return nil
	}
	if err := e.State.View(ctx, e.Repository, read); err != nil {
		return item, err
	}
	if dry || item.Action == "" {
		return item, nil
	}
	if item.Action == "release" {
		// Persist expiry before calling the existing claimed, recoverable release path.
		err := e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
			if err := read(ctx, tx); err != nil {
				return err
			}
			if item.Action != "release" {
				return nil
			}
			if resource.State == record.ResourceRetained {
				now := e.now()
				resource.RetainUntil = &now
				return tx.PutResource(ctx, resource)
			}
			return nil
		})
		if err != nil || item.Action != "release" {
			return item, err
		}
		c.checkProvider(ctx, resource.Handle.Provider)
		item.Detail, err = c.cleanup(ctx, id)
		if errors.Is(err, ErrClaimLost) || errors.Is(err, state.ErrConflict) {
			item.Detail, err = err.Error(), nil
		}
		if err != nil {
			return item, err
		}
		err = e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
			v, err := r.Resource(ctx, id)
			item.Completed = err == nil && v.State == record.ResourceReleased
			return err
		})
		if !item.Completed && item.Detail == "" {
			item.Detail = "release remains pending; another driver may own cleanup"
		}
		return item, err
	}
	pruner, ok := e.VerificationProvider(resource.Handle.Provider).(verify.ArtifactPruner)
	if !ok {
		item.Detail = "provider does not support artifact pruning"
		return item, nil
	}
	// Released ownership is immutable. The provider serializes idempotent removal;
	// no expiring workflow lease is needed for these already released files.
	callCtx, cancel := context.WithTimeout(ctx, c.timeouts.Cleanup)
	err := pruner.PruneArtifacts(callCtx, resource.Handle)
	if err == nil {
		err = callCtx.Err()
	}
	cancel()
	if err != nil {
		item.Detail = err.Error()
		return item, nil
	}
	err = e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		current, err := tx.Resource(ctx, id)
		if err != nil {
			return err
		}
		if current.State != record.ResourceReleased || current.Handle != resource.Handle {
			return state.ErrConflict
		}
		if current.ArtifactsPrunedAt == nil {
			now := e.now()
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
