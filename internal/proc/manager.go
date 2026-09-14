package proc

import (
	"context"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/workflow"
)

type Engine interface {
	Cycle(context.Context, workflow.Scope) (workflow.CycleResult, error)
	Status(context.Context, workflow.Scope) (workflow.Status, error)
}

type Manager struct {
	Interval time.Duration
	OnCycle  func(workflow.CycleResult) error
}

func (m *Manager) interval() (time.Duration, error) {
	if m.Interval < 0 {
		return 0, fmt.Errorf("proc: interval cannot be negative")
	}
	if m.Interval == 0 {
		return time.Second, nil
	}
	return m.Interval, nil
}
func (m *Manager) drive(ctx context.Context, step func() (done, progressed bool, err error)) error {
	interval, err := m.interval()
	if err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		done, progressed, err := step()
		if err != nil || done {
			return err
		}
		if progressed {
			continue
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func (m *Manager) cycle(ctx context.Context, e Engine, scope workflow.Scope) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	result, err := e.Cycle(ctx, scope)
	if err != nil {
		return false, err
	}
	if m.OnCycle != nil {
		if err = m.OnCycle(result); err != nil {
			return false, err
		}
	}
	return len(result.Advanced) > 0, nil
}

// Run advances work in the current process until its context is canceled.
// Claims in state allow multiple callers; there is no resident singleton lock.
func (m *Manager) Run(ctx context.Context, e Engine, scope workflow.Scope) error {
	return m.drive(ctx, func() (bool, bool, error) {
		progressed, err := m.cycle(ctx, e, scope)
		return false, progressed, err
	})
}

// Attach resumes a fixed job selection and returns its last recorded snapshot.
// Stopping attachment does not request cancellation of accepted work.
func (m *Manager) Attach(ctx context.Context, e Engine, scope workflow.Scope, milestone workflow.Milestone, observe func(workflow.Status) error) (workflow.Status, error) {
	if scope.All || len(scope.Jobs) == 0 || milestone != workflow.Admission && milestone != workflow.Completion {
		return workflow.Status{}, fmt.Errorf("proc: attachment requires explicit jobs and a valid milestone")
	}
	var status workflow.Status
	err := m.drive(ctx, func() (bool, bool, error) {
		current, err := e.Status(ctx, scope)
		if err != nil {
			return false, false, err
		}
		status = current
		if observe != nil {
			if err = observe(status); err != nil {
				return false, false, err
			}
		}
		if workflow.Reached(status, milestone) {
			return true, false, nil
		}
		progressed, err := m.cycle(ctx, e, scope)
		return false, progressed, err
	})
	return status, err
}
