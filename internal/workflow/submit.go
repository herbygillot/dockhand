package workflow

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/model"
)

type Request struct {
	ID   model.RequestID
	Spec model.JobSpec
}

type Receipt struct {
	RequestID  model.RequestID
	JobID      model.JobID
	AcceptedAt time.Time
}

// Submit will accept work only after its ledger transaction commits.
func (e *Engine) Submit(ctx context.Context, request Request) (Receipt, error) {
	return Receipt{}, ErrNotImplemented
}

func (e *Engine) Control(ctx context.Context, request model.ControlRequest) error {
	return ErrNotImplemented
}
