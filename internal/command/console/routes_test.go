package console_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// tidy's review diff, opened by "d" in a tidy the console runs on a narrow
// terminal, is the payload whole: a command run from the console kept the
// console's wrapper, begun again over it, and its diff wrapped (#94, the
// second of the three routes the issue names).
func TestTidysDiffFromTheConsoleIsWhole(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.WithBumper(t)
	fastConsole(t)
	testsupport.Git(t, w.Clone, "switch", "-q", "-c", "update-jq")
	long := "long_description    jq is a lightweight and flexible command-line JSON processor, akin to sed for JSON data"
	require.NoError(t, os.WriteFile(filepath.Join(w.Clone, "textproc/jq/Portfile"), []byte("name jq\n# a\n"), 0o644))
	testsupport.Git(t, w.Clone, "commit", "-q", "-am", "wip")
	require.NoError(t, os.WriteFile(filepath.Join(w.Clone, "textproc/jq/Portfile"), []byte("name jq\n# a\n"+long+"\n"), 0o644))
	testsupport.Git(t, w.Clone, "commit", "-q", "-am", "oops")
	_, _, err := cmdtest.Dockhand(t, "adopt")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	typed, typing := io.Pipe()
	defer typed.Close()
	defer typing.Close()
	var viewed cmdtest.SyncBuffer
	done := make(chan error, 1)
	go func() {
		done <- cmdtest.RunFor(t, ctx, []string{"console"}, term.Streams{In: typed, Out: &viewed, Err: &viewed}.AsTerminal().WithDisplay(term.Display{Columns: 40}))
	}()
	send := func(keys string) {
		_, err := io.WriteString(typing, keys)
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "dockhand console · ") }, cmdtest.Settle, 10*time.Millisecond)
	send("t")
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "Review diff [d]") }, cmdtest.Settle, 10*time.Millisecond, "%s", viewed.String())
	answerThroughTheConsole(t, &viewed, send, "Review diff [d]", "d")
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "+"+long+"\n") }, cmdtest.Settle, 10*time.Millisecond, "the diff's line, whole: %s", viewed.String())
	send("q\n")
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "Press any key to return to the console.") }, cmdtest.Settle, 10*time.Millisecond)
	send(" ")
	send("q")
	require.NoError(t, <-done)
}

// answerThroughTheConsole answers a command the console runs. Over a test's
// pipe the console's key reader may be waiting in a read the screen's
// suspension can't end, as a terminal's raw read of a tenth of a second
// does, and take the first line typed. So a line no prompt takes is typed,
// again until the command asks again for it, which shows input now reaches
// the command, and then the answer; a command that never asks again fails
// the test.
func answerThroughTheConsole(t *testing.T, viewed *cmdtest.SyncBuffer, send func(string), prompt, answer string) {
	t.Helper()
	asked := strings.Count(viewed.String(), prompt)
	reasked := func() bool { return strings.Count(viewed.String(), prompt) > asked }
	for tries := 0; tries < 3 && !reasked(); tries++ {
		send("z\n")
		for deadline := time.Now().Add(time.Second); time.Now().Before(deadline) && !reasked(); {
			time.Sleep(20 * time.Millisecond)
		}
	}
	require.True(t, reasked(), "the command never asked again: %s", viewed.String())
	send(answer + "\n")
}

// submit's preview, opened by "p" in a submit the console runs on a narrow
// terminal, is the Markdown body whole: the console's wrapper reached it
// (#94, the second route, for submit's preview).
func TestSubmitsPreviewFromTheConsoleIsWhole(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithGitHub(t, w)
	fastConsole(t)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	note := "A note whose one line runs well past a terminal's forty columns, as a command or a URL in a pull request's body does."
	cmdtest.WithScript(t, w, "passed")
	_, _, err = cmdtest.Dockhand(t, "ship", "jq", "--to", "check", "--note", note, "--tested-binaries")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	typed, typing := io.Pipe()
	defer typed.Close()
	defer typing.Close()
	var viewed cmdtest.SyncBuffer
	done := make(chan error, 1)
	go func() {
		done <- cmdtest.RunFor(t, ctx, []string{"console"}, term.Streams{In: typed, Out: &viewed, Err: &viewed}.AsTerminal().WithDisplay(term.Display{Columns: 40}))
	}()
	send := func(keys string) {
		_, err := io.WriteString(typing, keys)
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "dockhand console · ") }, cmdtest.Settle, 10*time.Millisecond)
	send("s")
	require.Eventually(t, func() bool {
		return strings.Contains(viewed.String(), "Preview [p]") || strings.Contains(viewed.String(), "Press any key")
	}, cmdtest.Settle, 10*time.Millisecond)
	require.Contains(t, viewed.String(), "Preview [p]", "submit asked: %s", viewed.String())
	answerThroughTheConsole(t, &viewed, send, "Preview [p]", "p")
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), note+"\n") }, cmdtest.Settle, 10*time.Millisecond, "the body's line, whole: %s", viewed.String())
	send("q\n")
	require.Eventually(t, func() bool { return strings.Contains(viewed.String(), "Press any key to return to the console.") }, cmdtest.Settle, 10*time.Millisecond)
	send(" ")
	send("q")
	require.NoError(t, <-done)
}
