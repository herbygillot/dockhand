package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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

// A cancel serve applies that can't be finished yet is that check's
// state, said, and serve goes on: it exited at once, and again at every
// restart, until the provider could see the work end (#331).
func TestServeGoesOnPastACancelItCantFinishYet(t *testing.T) {
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
		r.State, r.StartedAt, r.CancelRequested = model.RunRunning, &started, &started
		if err := tx.UpdateRun(r); err != nil {
			return err
		}
		return tx.AddExecution(model.GuestExecution{ID: "command_probe", Run: run.ID, Environment: tahoeArm, Attempt: 1, State: model.ExecutionRunning, CreatedAt: started, ProviderRef: "still-running"})
	}))
	var mu sync.Mutex
	var said []string
	err := e.Serve(t.Context(), session(t, e), ServeOptions{Drain: true, Poll: 10 * time.Millisecond, Say: func(line string) {
		mu.Lock()
		defer mu.Unlock()
		said = append(said, line)
	}})
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, said, "check-1: a cancel was asked, and command couldn't see still-running stop: work is still running; serve asks again as it passes")
	recorded, err := e.Run(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunRunning, recorded.State)
	require.NotNil(t, recorded.StopAsked)
}
