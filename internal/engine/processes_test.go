package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// The processes working on the checkout are listed, each with what it
// does, but the one asking, and none that ended.
func TestProcessesSayWhatEachDoes(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	serve := session(t, e)
	_, err := serve.Lead(t.Context())
	require.NoError(t, err)
	idle := session(t, e)
	ended := session(t, e)
	require.NoError(t, ended.End(t.Context()))
	asking := session(t, e)

	processes, err := e.Processes(t.Context(), asking)
	require.NoError(t, err)
	require.Len(t, processes, 2)
	require.Equal(t, serve.ID(), processes[0].Session.ID)
	require.True(t, processes[0].Leads)
	require.Equal(t, idle.ID(), processes[1].Session.ID)
	require.False(t, processes[1].Leads)
	require.Empty(t, processes[1].Runs)
	require.Zero(t, processes[0].Expects, "the same schema as the database's")
	require.Equal(t, model.SessionForeground, processes[0].Session.Kind)
	require.NotZero(t, processes[0].Session.Schema, "each session records the schema it expects")
}
