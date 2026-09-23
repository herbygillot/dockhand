package retention

import (
	"context"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

type Options struct {
	OlderThan time.Duration
	DryRun    bool
}

// Item is one action the collection took or previewed.
type Item struct {
	// Repository is set by callers that collect several registrations.
	Repository record.RepositoryID `json:"repository,omitempty"`
	ResourceID record.ResourceID   `json:"resource_id,omitempty"`
	AttemptID  record.AttemptID    `json:"attempt_id,omitempty"`
	Path       string              `json:"path,omitempty"`
	Action     string              `json:"action"`
	Completed  bool                `json:"completed"`
	Detail     string              `json:"detail,omitempty"`
}

// Result can describe partial progress when collection returns an error.
type Result struct {
	Before time.Time `json:"before"`
	DryRun bool      `json:"dry_run"`
	Items  []Item    `json:"items"`
}

// Collector holds what one collection needs. Release is the engine's
// claimed, recoverable release of one resource, given the resource and the
// provider it lives on; it returns the detail to report, and turns a lost
// claim or a conflict into a detail rather than an error. Provider routes a
// persisted provider name.
type Collector struct {
	State    state.Scoped
	Now      func() time.Time
	Repo     *git.Repository
	Provider func(name string) verify.Provider
	Release  func(ctx context.Context, id record.ResourceID, provider string) (string, error)
	// Timeout bounds one provider call.
	Timeout time.Duration
}

// Collect releases old terminal resources through the engine's release
// path, prunes older released diagnostics and log caches, and deletes the
// local branches of merged contributions. It never advances jobs or forgets
// their identities. Explicit collection also releases retained failed
// environments.
func (c *Collector) Collect(ctx context.Context, options Options) (Result, error) {
	result := Result{DryRun: options.DryRun, Items: []Item{}}
	if options.OlderThan < 0 {
		return result, fmt.Errorf("retention: age must not be negative")
	}
	result.Before = c.Now().Add(-options.OlderThan)
	q := state.Query{Limit: 64, CleanupBefore: &result.Before}
	for {
		var resources []record.Resource
		err := c.State.View(ctx, func(ctx context.Context, r state.Reader) error {
			var err error
			resources, err = r.Resources(ctx, q)
			return err
		})
		if err != nil {
			return result, err
		}
		for _, resource := range resources {
			item, err := c.CollectResource(ctx, resource.ID, result.Before, options.DryRun)
			if item.Action != "" {
				result.Items = append(result.Items, item)
			}
			if err != nil {
				return result, err
			}
		}
		if len(resources) < q.Limit {
			if err := c.collectLogCaches(ctx, &result); err != nil {
				return result, err
			}
			return result, c.collectMergedBranches(ctx, &result, options.DryRun)
		}
		q.After = string(resources[len(resources)-1].ID)
	}
}
