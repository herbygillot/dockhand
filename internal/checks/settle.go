package checks

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// LaunchLock is an execution's launch lock, also taken by clean before it
// removes a resource found in that execution's launch note (#241).
func (c *Service) LaunchLock(id model.ExecutionID) buildenv.LaunchLock {
	return launchLock{path: c.launchLockPath(id)}
}

// settle asks only after the environment's result is recorded and no
// attempt follows. Its words are progress, including any failure, never a
// change to the result or the driver's exit (#241).
func (c *Service) settle(ctx context.Context, session *coord.Session, lease model.Lease, execution model.GuestExecution) {
	provider, ok := c.Providers[execution.Environment.Provider].(buildenv.Settler)
	if !ok {
		return
	}
	ctx = context.WithoutCancel(ctx)
	var attempts []model.GuestExecution
	err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
		recorded, err := r.Execution(execution.ID)
		if err != nil {
			return err
		}
		execution = recorded
		all, err := r.Executions(execution.Run)
		for _, attempt := range all {
			if attempt.Environment.Provider == execution.Environment.Provider {
				attempts = append(attempts, attempt)
			}
		}
		return err
	})
	var said []string
	if err == nil {
		said, err = provider.Settle(ctx, buildenv.SettleRequest{Execution: execution, Attempts: attempts, Lock: c.LaunchLock(execution.ID)})
	}
	if err != nil && len(said) == 0 {
		said = append(said, fmt.Sprintf("couldn't settle %s; dockhand clean can try again: %v", DescribeEnvironment(execution.Environment), err))
	}
	for _, line := range said {
		_ = session.Fenced(ctx, lease, func(tx store.Tx) error {
			run, err := tx.Run(execution.Run)
			if err != nil {
				return err
			}
			_, err = session.Emit(tx, model.Event{Branch: run.Branch, Run: run.ID, Kind: model.EventProgress, Level: model.LevelInfo,
				Message: DescribeEnvironment(execution.Environment) + ": " + line})
			return err
		})
	}
}

// settle queues the provider's settlement at the boundary where no
// attempt follows; finish records the verdict before asking it (#241).
func (d *driver) settle(_ context.Context, execution model.GuestExecution) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Each request carries every attempt on the provider. A matrix's
	// cells share its branch and must not repeat a refused deletion.
	for i, earlier := range d.settling {
		if earlier.Environment.Provider == execution.Environment.Provider {
			if execution.Attempt > earlier.Attempt {
				d.settling[i] = execution
			}
			return
		}
	}
	d.settling = append(d.settling, execution)
}
