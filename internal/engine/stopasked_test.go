package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// cannotStop is a provider whose work outlives its driver and can't be
// seen to stop.
type cannotStop struct{ scriptedProvider }

func (*cannotStop) Stop(context.Context, buildenv.StopRequest) ([]string, error) {
	return nil, errors.New("work is still running")
}

// The last time a check's providers were asked to stop is kept where the
// next operation reads it, across processes: the engine makes a service
// for each operation, so serve's loop takes a cancel that isn't finished
// again only once a provider's poll has passed (Codex's review of #193
// steps 5 and 6, finding 6).
func TestTheLastStopAskIsKeptAcrossOperations(t *testing.T) {
	t.Parallel()
	e := setup(t).open(t)
	e.Providers = map[string]buildenv.Provider{"command": &cannotStop{}}
	run := queuedHarborRun(t, e, tahoeArm)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		r, err := tx.Run(run.ID)
		if err != nil {
			return err
		}
		started := e.now()
		r.State, r.StartedAt = model.RunRunning, &started
		if err := tx.UpdateRun(r); err != nil {
			return err
		}
		return tx.AddExecution(model.GuestExecution{ID: "command_probe", Run: run.ID, Environment: tahoeArm, Attempt: 1, State: model.ExecutionRunning, CreatedAt: started, ProviderRef: "still-running"})
	}))
	s := session(t, e)
	_, err := e.RequestCancel(t.Context(), s, run.ID)
	require.ErrorContains(t, err, "work is still running")
	candidates, err := e.Candidates(t.Context(), s)
	require.NoError(t, err)
	require.Empty(t, candidates, "asked just now: not again until a provider's poll has passed")
}
