package proc

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
)

var ErrNotImplemented = errors.New("proc: driver process management is not implemented")

type Manager struct {
	CommonDir string
}

type Process struct {
	ID        record.ProcessID
	PID       int
	Scope     workflow.Scope
	StartedAt time.Time
}

func (m *Manager) Discover(ctx context.Context) ([]Process, error) {
	return nil, ErrNotImplemented
}

// Run will execute persistent driver cycles in the current process.
func (m *Manager) Run(ctx context.Context, engine *workflow.Engine, scope workflow.Scope) error {
	return ErrNotImplemented
}
