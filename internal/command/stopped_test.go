package command

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// A check whose process was killed stays recorded as running, since
// nothing settled it. status and queue say it stopped, and how to resume
// it or end it, rather than that it runs; dockhand wait resumes it.
func TestAKilledCheckReadsAsStopped(t *testing.T) {
	checkedBranch(t)
	_, _, err := dockhand(t, "check", "-d")
	require.NoError(t, err)

	// The process that took the check up dies without settling it.
	ctx := t.Context()
	e, err := (&settings{}).open(ctx)
	require.NoError(t, err)
	defer e.Close()
	run, err := e.RunNamed(ctx, "check-1")
	require.NoError(t, err)
	killed, err := startSession(ctx, e, model.SessionForeground)
	require.NoError(t, err)
	lease, err := killed.Acquire(ctx, engine.RunResource(run.ID))
	require.NoError(t, err)
	require.NoError(t, killed.Fenced(ctx, lease, func(tx store.Tx) error {
		run.State = model.RunRunning
		return tx.UpdateRun(run)
	}))
	require.NoError(t, killed.End(context.WithoutCancel(ctx)))

	out, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  Checks   stopped (check-1)\n")
	require.Contains(t, out, "Next: dockhand wait check-1\n")
	out, _, err = dockhand(t, "status", "--all")
	require.NoError(t, err)
	require.Contains(t, out, "check-1 stopped: the process running it ended; dockhand cancel check-1 ends it")
	require.Contains(t, out, "dockhand wait check-1")
	require.Contains(t, out, "stopped (check-1)")
	require.Contains(t, out, "serve: not running · queue: 1 run, 1 stopped\n")
	result, err := jsonOf(t, "status")
	require.NoError(t, err)
	require.Equal(t, true, dig(t, result.Result, "branches", 0, "active_checks", 0, "stopped"))
	require.Equal(t, "running", dig(t, result.Result, "branches", 0, "active_checks", 0, "state"), "the record is left as it is")
	result, err = jsonOf(t, "status", "--all")
	require.NoError(t, err)
	require.Equal(t, map[string]any{"running": false, "queue": float64(1), "stopped": float64(1)}, result.Result["serve_state"])

	out, _, err = dockhand(t, "queue")
	require.NoError(t, err)
	require.Contains(t, out, "stopped  the process running it ended; dockhand wait check-1 resumes it, cancel ends it\n")
	result, err = jsonOf(t, "queue")
	require.NoError(t, err)
	require.Equal(t, true, dig(t, result.Result, "runs", 0, "stopped"))

	_, errs, err := dockhand(t, "wait", "check-1")
	require.NoError(t, err)
	require.Contains(t, errs, "check-1 runs here, since no dockhand serve is running.")
	out, _, err = dockhand(t, "status", "--all")
	require.NoError(t, err)
	require.NotContains(t, out, "stopped")
	require.Contains(t, out, "serve: not running · queue: empty\n")
}
