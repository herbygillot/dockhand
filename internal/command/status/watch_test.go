package status

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/stretchr/testify/require"
)

func fastWatch(t *testing.T) {
	t.Helper()
	cmdtest.Hosted(t).Timings.WatchPoll = 20 * time.Millisecond
}

func TestWatchPrintsEventsAsLinesWithoutATerminal(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	fastWatch(t)
	ctx, stop := context.WithCancel(t.Context())
	var watched cmdtest.SyncBuffer
	done := make(chan error)
	go func() {
		done <- cmdtest.RunFor(t, ctx, []string{"status", "--watch"}, term.Streams{In: strings.NewReader(""), Out: &watched, Err: &watched})
	}()
	require.Eventually(t, func() bool { return strings.Contains(watched.String(), "Watching for changes") }, cmdtest.Settle, 10*time.Millisecond)
	require.Contains(t, watched.String(), "jq-update", "status comes first")

	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return strings.Contains(watched.String(), "  jq-update  check-1 passed") }, cmdtest.Settle, 10*time.Millisecond)
	require.NotContains(t, watched.String(), "session", "sessions coming and going are not news")
	stop()
	require.NoError(t, <-done, "being stopped is how a watch ends")
}

func TestWatchRedrawsAndRunsWhatIsTyped(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	fastWatch(t)
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)

	typed, typing := io.Pipe()
	var viewed cmdtest.SyncBuffer
	done := make(chan error)
	go func() {
		done <- cmdtest.RunFor(t, t.Context(), []string{"status", "--watch"}, term.Streams{In: typed, Out: &viewed, Err: &viewed}.AsTerminal())
	}()
	draws := func() int { return strings.Count(viewed.String(), "dockhand status --watch · ") }
	require.Eventually(t, func() bool { return draws() == 1 }, cmdtest.Settle, 10*time.Millisecond)
	require.True(t, strings.HasPrefix(viewed.String(), enterView), "the view has a screen of its own")
	require.Contains(t, viewed.String(), "c check · l logs · t tidy · s submit, each <branch> · q quits")

	send := func(text string) {
		_, err := io.WriteString(typing, text+"\n")
		require.NoError(t, err)
	}
	send("x")
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), `"x": c, l, t, or s and a branch`) }, cmdtest.Settle, 10*time.Millisecond)

	before := draws()
	_, _, err = cmdtest.Dockhand(t, "archive")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return draws() > before }, cmdtest.Settle, 10*time.Millisecond, "a journaled change redraws the view")

	send("l")
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "Press Enter to return to status.") }, cmdtest.Settle, 10*time.Millisecond)
	require.Contains(t, viewed.String(), "$ dockhand logs check-1\n")
	before = draws()
	send("")
	require.Eventually(t, func() bool { return draws() > before }, cmdtest.Settle, 10*time.Millisecond)
	send("q")
	require.NoError(t, <-done)
	require.True(t, strings.HasSuffix(viewed.String(), leaveView), "and gives the terminal back")
}
