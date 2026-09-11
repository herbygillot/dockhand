package workflow

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/model"
)

type JobStatus struct {
	Job          model.Job
	Attempts     []model.Attempt
	Publications []model.PublicationAction
	Resources    []model.Resource
}

type Status struct {
	LedgerVersion model.ObjectID
	ReadAt        time.Time
	Jobs          []JobStatus
	Changes       []model.Change
	PullRequests  []model.PullRequest
}

// Status will project ledger records without advancing work or querying providers.
func (e *Engine) Status(ctx context.Context, scope Scope) (Status, error) {
	return Status{}, ErrNotImplemented
}
