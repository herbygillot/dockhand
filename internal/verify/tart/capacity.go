package tart

import (
	"context"

	"github.com/herbygillot/dockhand/internal/state"
)

// listingBlocked is the wait a submission reports while a running VM with an
// ASIF disk keeps Tart from listing, and so from counting, its VMs.
const listingBlocked = "Waiting for Tart: a running VM with an ASIF disk keeps it from listing its VMs until that VM stops (openai/tart#1344)"

func (o *operation) checkCapacity(ctx context.Context) error {
	running, err := o.machine.Running(ctx)
	if err != nil {
		return err
	}
	return o.entry.View(ctx, func(ctx context.Context, r state.ProviderReader) error {
		return capacityAvailable(ctx, r, running, o.entry.Pool.Capacity)
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
