package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A session beats at the interval the options give, the coordinator's
// default where none is: the fault sweep's worlds set one no run lasts,
// so no heartbeat falls inside a run (docs/designs/fault-sweep.md,
// "A fault is placed at an address"; #129's second round).
func TestASessionBeatsAtTheOptionsInterval(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.options.Heartbeat = 10 * time.Millisecond
	e := f.open(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	session, err := e.Session(ctx, model.SessionForeground)
	require.NoError(t, err)
	started := session.Record().HeartbeatAt
	// Well inside the default's five seconds.
	require.Eventually(t, func() bool { return session.Record().HeartbeatAt.After(started) }, 2*time.Second, 10*time.Millisecond)
}
