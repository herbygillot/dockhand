package app_test

import (
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/stretchr/testify/require"
)

func TestRecordCapacityRegistersThenChangesThePoolLimit(t *testing.T) {
	t.Parallel()
	home, directory := t.TempDir(), t.TempDir()
	config := app.Config{DBPath: filepath.Join(t.TempDir(), "state.db"), Tart: tart.Config{Home: home, ArtifactDirectory: directory, Executable: "/missing/tart"}}
	_, err := app.RecordCapacity(t.Context(), config, 0)
	require.ErrorContains(t, err, "positive")

	first, err := app.RecordCapacity(t.Context(), config, 3)
	require.NoError(t, err)
	require.Equal(t, app.CapacityChange{Limit: 3}, first, "a pool not yet recorded is registered with the limit")
	second, err := app.RecordCapacity(t.Context(), config, 5)
	require.NoError(t, err)
	require.Equal(t, app.CapacityChange{Previous: 3, Limit: 5}, second)
	same, err := app.RecordCapacity(t.Context(), config, 5)
	require.NoError(t, err)
	require.Equal(t, app.CapacityChange{Previous: 5, Limit: 5}, same)

	pool, err := tart.Pool(config.Tart)
	require.NoError(t, err)
	store, err := sqlite.Open(t.Context(), config.DBPath, sqlite.Options{ReadOnly: true})
	require.NoError(t, err)
	defer store.Close()
	recorded, err := store.ProviderPool(t.Context(), pool.ID)
	require.NoError(t, err)
	require.Equal(t, 5, recorded.Capacity)
	require.Equal(t, pool.Directory, recorded.Directory)

	moved := config
	moved.Tart.ArtifactDirectory = t.TempDir()
	_, err = app.RecordCapacity(t.Context(), moved, 2)
	require.ErrorContains(t, err, "keeps its artifacts in", "the pool's identity is not rewritten by a capacity change")
}
