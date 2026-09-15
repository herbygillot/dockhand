package tart

import (
	"context"

	"github.com/herbygillot/dockhand/internal/state"
)

func (o *operation) checkCapacity(ctx context.Context) error {
	running, err := o.machine.Running(ctx)
	if err != nil {
		return err
	}
	return o.provider.State.ProviderView(ctx, o.pool.ID, func(ctx context.Context, r state.ProviderReader) error {
		return capacityAvailable(ctx, r, running, o.pool.Capacity)
	})
}

func capacityAvailable(ctx context.Context, r state.ProviderReader, running []string, capacity int) error {
	occupied, err := r.Occupied(ctx)
	if err != nil {
		return err
	}
	names := make(map[string]bool)
	for _, name := range running {
		names[name] = true
	}
	for _, run := range occupied {
		names[run.Resource] = true
	}
	if len(names) >= capacity {
		return errCapacity
	}
	return nil
}
