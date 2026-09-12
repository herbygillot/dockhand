package perftest_test

import (
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/perftest"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestFixtureSupportsCompletionCleanupAndReset(t *testing.T) {
	f, err := perftest.New(t.Context(), t.TempDir(), 2, 2, 1)
	require.NoError(t, err)
	before, err := f.Store.Read(t.Context())
	require.NoError(t, err)
	e := workflow.Engine{Ledger: f.Store, Provider: &perftest.Provider{Finish: true}}
	result, err := e.Cycle(t.Context(), workflow.Scope{Jobs: f.Active})
	require.NoError(t, err)
	require.Empty(t, result.Problems)
	after, err := f.Store.Read(t.Context())
	require.NoError(t, err)
	require.Equal(t, record.JobCompleted, after.State.Jobs[f.Active[0]].State)
	for _, resource := range after.State.Resources {
		require.Equal(t, record.ResourceReleased, resource.State)
	}
	require.NoError(t, f.AddRevision(t.Context()))
	pins, err := f.Repo.ReadRefs(t.Context(), ledger.PinsPrefix)
	require.NoError(t, err)
	require.Len(t, pins, 6)
	require.NoError(t, f.Reset(t.Context()))
	reset, err := f.Store.Read(t.Context())
	require.NoError(t, err)
	require.Equal(t, before, reset)
	pins, err = f.Repo.ReadRefs(t.Context(), ledger.PinsPrefix)
	require.NoError(t, err)
	require.Len(t, pins, 4)
}

func TestHistoryAndPackingPreserveCurrentState(t *testing.T) {
	f, err := perftest.New(t.Context(), t.TempDir(), 2, 1, 0)
	require.NoError(t, err)
	before, err := f.Store.Read(t.Context())
	require.NoError(t, err)
	require.NoError(t, f.AddHistory(t.Context(), 10))
	_, err = perftest.Git(t.Context(), f.Repo.Root, nil, "gc", "--quiet")
	require.NoError(t, err)
	after, err := f.Store.Read(t.Context())
	require.NoError(t, err)
	require.Equal(t, before.State, after.State)
	require.NotEqual(t, before.Version, after.Version)
	require.Equal(t, f.Version, string(after.Version))
	require.NoError(t, f.AddRevision(t.Context()), "reserved source must survive packing")
}
