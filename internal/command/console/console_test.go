package console_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/term"
)

func fastConsole(t *testing.T) {
	t.Helper()
	cmdtest.Hosted(t).Timings.WatchPoll = 20 * time.Millisecond
}

// Without a terminal, and with --plain on one, the console prints status
// once and then each event as a line.
func TestThePlainConsolePrintsStatusThenEvents(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		args    []string
		streams func(io.Reader, io.Writer) term.Streams
	}{
		{"no terminal", []string{"console"}, func(in io.Reader, out io.Writer) term.Streams { return term.Streams{In: in, Out: out, Err: out} }},
		{"--plain", []string{"console", "--plain"}, func(in io.Reader, out io.Writer) term.Streams {
			return term.Streams{In: in, Out: out, Err: out}.AsTerminal()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmdtest.CheckedBranch(t)
			fastConsole(t)
			ctx, stop := context.WithCancel(t.Context())
			var watched cmdtest.SyncBuffer
			done := make(chan error)
			go func() {
				done <- cmdtest.RunFor(t, ctx, tc.args, tc.streams(strings.NewReader(""), &watched))
			}()
			require.Eventually(t, func() bool { return strings.Contains(watched.String(), "Watching for changes") }, cmdtest.Settle, 10*time.Millisecond)
			require.Contains(t, watched.String(), "jq-update", "status comes first")
			require.NotContains(t, watched.String(), "\x1b[", "no screen of its own")

			_, _, err := cmdtest.Dockhand(t, "check")
			require.NoError(t, err)
			require.Eventually(t, func() bool { return strings.Contains(watched.String(), "  jq-update  check-1 passed") }, cmdtest.Settle, 10*time.Millisecond)
			require.NotContains(t, watched.String(), "session", "sessions coming and going are not news")
			stop()
			require.NoError(t, <-done, "being stopped is how it ends")
		})
	}
}

// On a terminal, the console draws the table on a screen of its own, the
// row selected reversed; Enter shows the branch in the pane, a journaled
// change is gathered again, and q gives the terminal back.
func TestTheConsoleDrawsTheTableAndActsOnKeys(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	fastConsole(t)
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)

	typed, typing := io.Pipe()
	defer typed.Close()
	var viewed cmdtest.SyncBuffer
	done := make(chan error, 1)
	go func() {
		done <- cmdtest.RunFor(t, t.Context(), []string{"console"}, term.Streams{In: typed, Out: &viewed, Err: &viewed}.AsTerminal())
	}()
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "dockhand console · ") }, cmdtest.Settle, 10*time.Millisecond)
	require.True(t, strings.HasPrefix(viewed.String(), "\x1b[?1049h"), "the console has a screen of its own")
	require.Contains(t, viewed.String(), "BRANCH")
	require.Contains(t, viewed.String(), "\x1b[7m✓ jq-update  jq 1.8.1", "the branch ready for tidy, selected")
	require.Contains(t, viewed.String(), "↑↓ select · PgUp PgDn page · Enter detail · l logs · c check · t tidy · s submit · r refresh · q qu…", "the keys, cut at the width")

	send := func(keys string) {
		_, err := io.WriteString(typing, keys)
		require.NoError(t, err)
	}
	send("\n")
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "── jq-update ─") }, cmdtest.Settle, 10*time.Millisecond, "Enter shows the branch in the pane")
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "Next: dockhand tidy --branch jq-update") }, cmdtest.Settle, 10*time.Millisecond)
	send("\n")
	require.Eventually(t, func() bool {
		drawn := viewed.String()
		return strings.LastIndex(drawn, "Enter detail") > strings.LastIndex(drawn, "Enter events")
	}, cmdtest.Settle, 10*time.Millisecond, "Enter again returns to the events")

	_, _, err = cmdtest.Dockhand(t, "archive")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "No open branches.") }, cmdtest.Settle, 10*time.Millisecond, "a journaled change is gathered again")
	send("q")
	require.NoError(t, <-done)
	require.True(t, strings.HasSuffix(viewed.String(), "\x1b[?25h\x1b[?1049l"), "and gives the terminal back")
}

// A command the console runs runs in the console's own invocation, its
// environment, directory, tree, and database, where it ran in the
// process's and failed, or could act on another checkout's branch of the
// same name (Codex's review of 8c007979, C8-1, its probe); it says itself
// first, and a key returns to the table.
func TestAConsolesCommandRunsInTheConsolesInvocation(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	cmdtest.Hosted(t).Timings.WatchPoll = 10 * time.Millisecond
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	typed, typing := io.Pipe()
	defer typed.Close()
	defer typing.Close()
	var viewed cmdtest.SyncBuffer
	done := make(chan error, 1)
	go func() {
		done <- cmdtest.RunFor(t, ctx, []string{"console"}, term.Streams{In: typed, Out: &viewed, Err: &viewed}.AsTerminal())
	}()
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "dockhand console · ") }, cmdtest.Settle, 10*time.Millisecond)
	_, err = io.WriteString(typing, "l")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "Press any key to return to the console.") }, cmdtest.Settle, 10*time.Millisecond)
	observed := viewed.String()
	before := strings.Count(observed, "\x1b[?1049h")
	_, err = io.WriteString(typing, " ")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return strings.Count(viewed.String(), "\x1b[?1049h") > before }, cmdtest.Settle, 10*time.Millisecond, "a key returns to the table")
	_, err = io.WriteString(typing, "q")
	require.NoError(t, err)
	require.NoError(t, <-done)
	require.Contains(t, observed, "\x1b[?1049l\x1b[H\x1b[2J$ dockhand logs check-1\n", "said as typed, off the table's screen")
	require.NotContains(t, observed, "dockhand:", "the logs of the check the console shows, from its tree and database")
}

// status --watch and its --plain are the console's now, and say so.
func TestStatusWatchIsTheConsoleNow(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "status", "--watch")
	require.EqualError(t, err, "status --watch is now dockhand console")
	_, _, err = cmdtest.Dockhand(t, "status", "--plain")
	require.EqualError(t, err, "status --plain is now dockhand console --plain")
}
