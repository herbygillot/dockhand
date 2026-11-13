package command

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// hangupDir is where the test binary, run again as a check's dockhand,
// keeps what the test reads of it.
const hangupDir = "DOCKHAND_TEST_HANGUP_DIR"

// A hangup, which a closed terminal or a dropped SSH session sends, stops
// a check as an interrupt does: the build it started is stopped and the
// command unwinds to its end, where the process ended on the spot and left
// the build running, as it leaves a Tart check's VM and clone (#240). A
// terminal gone before the hangup, its output a pipe no one reads, doesn't
// end the unwind either.
func TestAHangupUnwindsACheck(t *testing.T) {
	t.Parallel()
	if dir := os.Getenv(hangupDir); dir != "" {
		hungUpCheck(t, dir)
		return
	}
	for _, gone := range []bool{false, true} {
		name := "terminal"
		if gone {
			name = "terminal gone"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			child := exec.Command(os.Args[0], "-test.run", "^TestAHangupUnwindsACheck$", "-test.count=1")
			child.Env = append(os.Environ(), hangupDir+"="+dir)
			var output bytes.Buffer
			var reader, writer *os.File
			if gone {
				var err error
				reader, writer, err = os.Pipe()
				require.NoError(t, err)
				child.Stdout, child.Stderr = writer, writer
			} else {
				child.Stdout, child.Stderr = &output, &output
			}
			require.NoError(t, child.Start())
			ended := make(chan error, 1)
			go func() { ended <- child.Wait() }()
			t.Cleanup(func() { _ = child.Process.Kill() })
			if gone {
				require.NoError(t, writer.Close(), "the child holds the terminal's end")
			}

			var build int
			require.Eventually(t, func() bool {
				data, err := os.ReadFile(filepath.Join(dir, "build.pid"))
				build, _ = strconv.Atoi(strings.TrimSpace(string(data)))
				return err == nil && build > 0
			}, testsupport.Settle, 20*time.Millisecond, "the check started its build")
			t.Cleanup(func() { _ = syscall.Kill(build, syscall.SIGKILL) })
			if gone {
				require.NoError(t, reader.Close(), "the terminal is gone")
			}
			require.NoError(t, child.Process.Signal(syscall.SIGHUP))

			var err error
			select {
			case err = <-ended:
			case <-time.After(testsupport.Settle):
				t.Fatal("dockhand didn't end after the hangup")
			}
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				status, _ := exit.Sys().(syscall.WaitStatus)
				require.False(t, status.Signaled(), "dockhand ended by %v, not by its own exit: %s", status.Signal(), output.String())
			}
			require.NoError(t, err, output.String())
			outcome, readErr := os.ReadFile(filepath.Join(dir, "outcome"))
			require.NoError(t, readErr, "the command unwound to its end: %s", output.String())
			require.Equal(t, "check-1 stopped; finished results are kept", string(outcome))
			if !gone {
				require.Contains(t, output.String(), "  jq  ! not run\n", "the unwind says what it left")
			}
			require.Eventually(t, func() bool {
				return syscall.Kill(build, 0) != nil
			}, testsupport.Settle, 50*time.Millisecond, "the build is stopped with the check")
		})
	}
}

// hungUpCheck is the test binary as dockhand running a check in the
// foreground, its build waiting until it's stopped: its context is the
// process's, as main's is, and what the command returned is kept in dir.
func hungUpCheck(t *testing.T, dir string) {
	w := cmdtest.CheckedBranch(t)
	testsupport.WriteExecutable(t, filepath.Join(w.Home, "bin", "build-ports"), "#!/bin/sh\necho $$ > '"+filepath.Join(dir, "build.pid")+"'\nexec sleep 300\n")
	ctx, stop := term.UntilStopped(context.Background())
	defer stop()
	err := cmdtest.RunFor(t, ctx, []string{"check"}, term.Streams{In: strings.NewReader(""), Out: os.Stdout, Err: os.Stderr})
	message := "no error"
	if err != nil {
		message = err.Error()
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outcome"), []byte(message), 0o600))
}
