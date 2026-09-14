package workflow

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// Scope selects all jobs or a nonempty list of explicit job IDs. Those forms
// are mutually exclusive. Status and Cycle collapse duplicate IDs and reject
// unknown jobs. All is limited to the engine's registered repository.
type Scope struct {
	All  bool
	Jobs []record.JobID
}

func validateScope(scope Scope) error {
	if scope.All == (len(scope.Jobs) > 0) {
		return ErrInvalidScope
	}
	for _, id := range scope.Jobs {
		if id == "" {
			return ErrInvalidScope
		}
	}
	return nil
}

func (e *Engine) checkScope(scope Scope) error {
	if err := validateScope(scope); err != nil {
		return err
	}
	if e == nil || e.State == nil || e.Repository == "" {
		return ErrNoState
	}
	return nil
}

func checkJobs(ctx context.Context, r state.Reader, scope Scope) error {
	for _, id := range scope.Jobs {
		if _, err := r.Job(ctx, id); err != nil {
			return fmt.Errorf("job %s: %w", id, err)
		}
	}
	return nil
}
