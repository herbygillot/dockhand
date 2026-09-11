package workflow

import (
	"context"

	"github.com/herbygillot/dockhand/v2/internal/model"
)

type JobProblem struct {
	JobID  model.JobID
	Detail string
}

type CycleResult struct {
	Advanced       []model.JobID
	Problems       []JobProblem
	PendingCleanup []model.ResourceID
}

func (e *Engine) Cycle(ctx context.Context, scope Scope) (CycleResult, error) {
	return CycleResult{}, ErrNotImplemented
}
