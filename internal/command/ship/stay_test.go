package ship

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/term"
)

// serving runs serve until the test ends, once it leads.
func serving(t *testing.T) *cmdtest.SyncBuffer {
	t.Helper()
	ctx, stop := context.WithCancel(t.Context())
	served := &cmdtest.SyncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- cmdtest.RunFor(t, ctx, []string{"serve"}, term.Streams{In: strings.NewReader(""), Out: served, Err: served})
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
	require.Eventually(t, func() bool { return strings.Contains(served.String(), "serve: leading") }, cmdtest.Settle, 10*time.Millisecond)
	return served
}

// ship's rows of the Staying matrix (docs/designs/check-lifecycle.md): a
// single change's new check, seen to its end through See, is handed to
// the serve that leads and followed, never driven here. Nobody leading,
// ship drives it here (TestShipCarriesAPortToItsPullRequest).
func TestShipFollowsTheCheckServeDrives(t *testing.T) {
	t.Parallel()
	g := twoPortWorld(t)
	served := serving(t)

	out, errs, err := cmdtest.Dockhand(t, "ship", "jq", "--yes")
	require.NoError(t, err, errs)
	require.Contains(t, out, "  ✓ checked    passed on command\n  ✓ submitted  opened #34901")
	require.NotContains(t, out+errs, "is running here", "serve leads, so nothing is driven here")
	require.Contains(t, served.String(), "check-1 jq-1.8.1: passed\n", "serve drove it")
	require.Len(t, g.PRs, 1)
}

// A batch's checks are followed together as the serve that leads drives
// them, then each is reported through See as a check that had already
// ended. Nobody leading, ship drives them here
// (TestShipQueuesABatchsChecksFirst).
func TestShipFollowsABatchServeDrives(t *testing.T) {
	t.Parallel()
	twoPortWorld(t)
	served := serving(t)

	var both bytes.Buffer
	err := cmdtest.RunFor(t, t.Context(), []string{"ship", "jq", "yq", "--yes", "--to", "check", "-v"}, term.Streams{In: strings.NewReader(""), Out: &both, Err: &both})
	out := both.String()
	require.NoError(t, err, out)
	require.Contains(t, out, "check-1 and check-2 are checked as a batch")
	require.NotContains(t, out, "is running here", "serve leads, so nothing is driven here")
	require.Equal(t, 2, strings.Count(out, "\nPassed.\n"), "each reported")
	require.Contains(t, served.String(), "check-1 jq-1.8.1: passed\n", "serve drove it")
	require.Contains(t, served.String(), "check-2 yq-1.8.1: passed\n", "serve drove it")
}
