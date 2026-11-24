package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// heldBuild makes the world's build wait until the test releases it,
// writing its PID first; release lets every build after it through.
func heldBuild(t *testing.T, w cmdtest.World) (pid func() int, release func()) {
	t.Helper()
	dir := t.TempDir()
	testsupport.WriteExecutable(t, filepath.Join(w.Home, "bin", "build-ports"), `#!/bin/sh
echo $$ > '`+filepath.Join(dir, "build.pid")+`'
while [ ! -f '`+filepath.Join(dir, "release")+`' ]; do sleep 0.05; done
cat > "$(dirname "$1")/result.json" <<JSON
{"version": 1, "targets": [{"id": "jq", "outcome": "passed"}]}
JSON
`)
	return func() int {
			data, err := os.ReadFile(filepath.Join(dir, "build.pid"))
			if err != nil {
				return 0
			}
			n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
			return n
		}, func() {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "release"), nil, 0o600))
		}
}

// startServe runs serve until the test ends, once it leads.
func startServe(t *testing.T) *cmdtest.SyncBuffer {
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

// interrupted runs a command line until errs says want, then cancels it,
// as Ctrl-C cancels the command's context, and returns what it ended with.
func interrupted(t *testing.T, want string, ready func() bool, args ...string) (string, error) {
	t.Helper()
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	var out bytes.Buffer
	var errs cmdtest.SyncBuffer
	done := make(chan error, 1)
	go func() {
		done <- cmdtest.RunFor(t, ctx, args, term.Streams{In: strings.NewReader(""), Out: &out, Err: &errs})
	}()
	require.Eventually(t, func() bool { return strings.Contains(errs.String(), want) && (ready == nil || ready()) }, cmdtest.Settle, 10*time.Millisecond, "%v said %q", args, want)
	stop()
	select {
	case err := <-done:
		return out.String() + errs.String(), err
	case <-time.After(cmdtest.Settle):
		t.Fatalf("%v didn't end after its Ctrl-C: %s", args, errs.String())
		return "", nil
	}
}

// Ctrl-C through See (docs/designs/check-lifecycle.md at bbb4c0b7, "Staying", and
// "Stopping": the three stay three). A check driven here because nobody
// leads it is stopped, its build with it, keeping what finished. One
// another drives, followed to its end or for a bound, is only no longer
// watched: the command exits 130 with the wait that follows it again,
// and the check goes on to its end where it is driven.
func TestCtrlCStopsWatchingAndStopsOnlyACheckDrivenHere(t *testing.T) {
	t.Parallel()
	w := cmdtest.CheckedBranch(t)
	build, release := heldBuild(t, w)

	// Nobody leads: check drives it here, and Ctrl-C stops it.
	said, err := interrupted(t, "check-1 is running here", func() bool { return build() > 0 }, "check")
	require.EqualError(t, err, "check-1 stopped; finished results are kept", said)
	require.Equal(t, 130, term.ExitCode(err), "a stopped check exits as interrupted")
	stopped := build()
	require.Eventually(t, func() bool { return syscall.Kill(stopped, 0) != nil }, cmdtest.Settle, 20*time.Millisecond, "the build stopped with the check")
	out, _, err := cmdtest.Dockhand(t, "status", "--all", "-v")
	require.NoError(t, err)
	require.NotContains(t, out, "Checks queued and running", "nothing is left running")

	// serve leads: a new check, to its end, is handed to serve; Ctrl-C
	// stops only the watching.
	served := startServe(t)
	said, err = interrupted(t, "check-2 was handed to serve (PID ", func() bool { return build() != stopped && build() > 0 }, "check", "-v")
	require.EqualError(t, err, "stopped following check-2; it continues\nNext: dockhand wait check-2", said)
	require.Equal(t, 130, term.ExitCode(err))
	// A joined check followed for a bound: the same.
	said, err = interrupted(t, "Following check-2 for 9m.", nil, "wait", "check-2", "--for", "9m")
	require.EqualError(t, err, "stopped following check-2; it continues\nNext: dockhand wait check-2", said)
	// A joined check followed to its end: the same.
	said, err = interrupted(t, "Following check-2.", nil, "wait", "check-2")
	require.EqualError(t, err, "stopped following check-2; it continues\nNext: dockhand wait check-2", said)
	require.NotContains(t, served.String(), "check-2 jq-update: canceled")

	release()
	out, _, err = cmdtest.Dockhand(t, "wait", "check-2")
	require.NoError(t, err)
	require.Contains(t, out, "Passed for snapshot 1.", "serve saw it to its end")
	require.Eventually(t, func() bool { return strings.Contains(served.String(), "check-2 jq-update: passed\n") }, cmdtest.Settle, 10*time.Millisecond)
}

// retry's rows of the Staying matrix: a new check, to its end or
// detached, with nobody leading and with serve leading. Nobody leading,
// to its end, it runs here (TestEditRevbumpRetryRebaseAndArchive);
// detached, a worker is started and named; with serve leading, it is
// handed to serve and followed, or queued for serve and left. -d's
// output names the new check once, on the line that says who runs it: it
// had said "check-2 repeats check-1" above it, breaking "said once".
func TestRetryStaysAsSeeSays(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)

	var drained int
	cmdtest.Hosted(t).StartDrain = func(engine.Options) (int, error) { drained++; return 4242, nil }
	out, _, err := cmdtest.Dockhand(t, "retry", "check-1", "-d")
	require.NoError(t, err)
	require.Contains(t, out, "Repeating check-1: snapshot 1\ncheck-2 is queued; a background worker (PID 4242) will run it. Follow it with dockhand wait check-2.\n")
	require.Equal(t, 1, drained, "nobody leads, so a worker was started")
	_, _, err = cmdtest.Dockhand(t, "wait", "check-2")
	require.NoError(t, err)

	served := startServe(t)
	out, errs, err := cmdtest.Dockhand(t, "retry", "check-1", "-v")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "check-3 was handed to serve (PID ")
	require.Contains(t, out, "\nPassed.\n")
	require.Contains(t, served.String(), "check-3 jq-update: passed\n", "serve drove it")
	out, _, err = cmdtest.Dockhand(t, "retry", "check-1", "-d")
	require.NoError(t, err)
	require.Contains(t, out, "check-4 is queued; serve (PID ")
	require.Equal(t, 1, drained, "serve leads, so no worker was started")
	require.Eventually(t, func() bool { return strings.Contains(served.String(), "check-4 jq-update: passed\n") }, cmdtest.Settle, 10*time.Millisecond)
}
