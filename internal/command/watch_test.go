package command

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fastWatch(t *testing.T) {
	t.Helper()
	poll := watchPoll
	t.Cleanup(func() { watchPoll = poll })
	watchPoll = 20 * time.Millisecond
}

func TestWatchPrintsEventsAsLinesWithoutATerminal(t *testing.T) {
	checkedBranch(t)
	fastWatch(t)
	ctx, stop := context.WithCancel(t.Context())
	var watched syncBuffer
	done := make(chan error)
	go func() {
		done <- Run(ctx, []string{"watch"}, Streams{In: strings.NewReader(""), Out: &watched, Err: &watched})
	}()
	require.Eventually(t, func() bool { return strings.Contains(watched.String(), "Watching; events follow") }, 5*time.Second, 10*time.Millisecond)
	require.Contains(t, watched.String(), "jq-update", "status comes first")

	_, _, err := dockhand(t, "check")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return strings.Contains(watched.String(), "  jq-update  check-1 passed") }, 5*time.Second, 10*time.Millisecond)
	require.NotContains(t, watched.String(), "session", "sessions coming and going are not news")
	stop()
	require.NoError(t, <-done, "being stopped is how watch ends")
}

func TestWatchRedrawsAndRunsWhatIsTyped(t *testing.T) {
	checkedBranch(t)
	fastWatch(t)
	_, _, err := dockhand(t, "check")
	require.NoError(t, err)

	typed, typing := io.Pipe()
	var view syncBuffer
	done := make(chan error)
	go func() {
		done <- Run(t.Context(), []string{"watch"}, Streams{In: typed, Out: &view, Err: &view, interactive: true})
	}()
	draws := func() int { return strings.Count(view.String(), "dockhand watch · ") }
	require.Eventually(t, func() bool { return draws() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.True(t, strings.HasPrefix(view.String(), enterView), "the view has a screen of its own")
	require.Contains(t, view.String(), "c check · l logs · t tidy · s submit, each <branch> · q quits")

	send := func(text string) {
		_, err := io.WriteString(typing, text+"\n")
		require.NoError(t, err)
	}
	send("x")
	require.Eventually(t, func() bool { return strings.Contains(view.String(), `"x": c, l, t, or s and a branch`) }, 5*time.Second, 10*time.Millisecond)

	before := draws()
	_, _, err = dockhand(t, "archive")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return draws() > before }, 5*time.Second, 10*time.Millisecond, "a journaled change redraws the view")

	send("l")
	require.Eventually(t, func() bool { return strings.Contains(view.String(), "Enter returns to the view.") }, 5*time.Second, 10*time.Millisecond)
	require.Contains(t, view.String(), "$ dockhand logs check-1\n")
	before = draws()
	send("")
	require.Eventually(t, func() bool { return draws() > before }, 5*time.Second, 10*time.Millisecond)
	send("q")
	require.NoError(t, <-done)
	require.True(t, strings.HasSuffix(view.String(), leaveView), "and gives the terminal back")
}
