package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

// CapacityChange reports the Tart pool's recorded limit before and after
// setup recorded one; a zero Previous means the pool was registered by it.
type CapacityChange struct {
	Previous int `json:"previous,omitempty"`
	Limit    int `json:"limit"`
}

// RecordCapacity records how many Tart guests may build at once on this Tart
// home, which every driver sharing the home reads. It is the one thing setup
// writes to the state database. A pool not yet recorded is registered with
// the limit; a recorded pool keeps its identity and takes the new limit. A
// driver already running keeps the limit it started with until it restarts.
func RecordCapacity(ctx context.Context, config Config, limit int) (CapacityChange, error) {
	if limit <= 0 {
		return CapacityChange{}, fmt.Errorf("capacity must be positive")
	}
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{})
	if err != nil {
		return CapacityChange{}, err
	}
	defer store.Close()
	settings := config.Tart
	settings.Capacity = limit
	if settings.ArtifactDirectory == "" {
		settings.ArtifactDirectory = filepath.Join(filepath.Dir(store.Path()), "artifacts", "tart")
	}
	pool, err := tart.Pool(settings)
	if err != nil {
		return CapacityChange{}, err
	}
	existing, err := store.ProviderPool(ctx, pool.ID)
	if errors.Is(err, state.ErrNotFound) {
		_, err = store.RegisterProviderPool(ctx, pool)
		return CapacityChange{Limit: limit}, err
	}
	if err != nil {
		return CapacityChange{}, err
	}
	if existing.Directory != pool.Directory {
		return CapacityChange{}, fmt.Errorf("tart: the recorded pool keeps its artifacts in %s, not %s", existing.Directory, pool.Directory)
	}
	if existing.Capacity != limit {
		if err := store.SetProviderPoolCapacity(ctx, pool.ID, limit); err != nil {
			return CapacityChange{}, err
		}
	}
	return CapacityChange{Previous: existing.Capacity, Limit: limit}, nil
}
