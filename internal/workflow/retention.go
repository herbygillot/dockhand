package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow/retention"
)

// Collect releases old terminal resources through the driver's existing
// cleanup path and prunes older released diagnostics, log caches, and the
// local branches of merged contributions. It never advances jobs or forgets
// their identities. Explicit collection also releases retained failed
// environments.
func (e *Engine) Collect(ctx context.Context, options retention.Options) (retention.Result, error) {
	if err := e.checkScope(Scope{All: true}); err != nil {
		return retention.Result{DryRun: options.DryRun, Items: []retention.Item{}}, err
	}
	c, err := e.newCycle()
	if err != nil {
		return retention.Result{DryRun: options.DryRun, Items: []retention.Item{}}, err
	}
	return c.collector().Collect(ctx, options)
}

// collector is the retention package's view of this cycle: the store, the
// repository, provider routing, and the claimed release path.
func (c *cycle) collector() *retention.Collector {
	e := c.engine
	return &retention.Collector{State: e.State, Now: e.now, Repo: e.Repo, Timeout: c.timeouts.Cleanup,
		Provider: func(name string) verify.Provider { return e.verificationProvider(name) },
		Release: func(ctx context.Context, id record.ResourceID, provider string) (string, error) {
			c.checkProvider(ctx, provider)
			detail, err := c.cleanup(ctx, id)
			if errors.Is(err, ErrClaimLost) || errors.Is(err, state.ErrConflict) {
				return err.Error(), nil
			}
			return detail, err
		}}
}

var _ = time.Second
