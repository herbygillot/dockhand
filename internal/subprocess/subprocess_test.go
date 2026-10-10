package subprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tool")
	testsupport.WriteExecutable(t, path, "#!/bin/sh\n"+body)
	return path
}

func TestRunCapturesStreamsAndReportsFailures(t *testing.T) {
	path := script(t, "echo out; echo err >&2; exit 3\n")
	result, err := Run(t.Context(), Spec{Tool: "fixture", Path: path, Args: []string{"sub", "arg"}})
	require.Equal(t, "out\n", string(result.Output))
	require.Equal(t, "err\n", string(result.Stderr))
	var failure *Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "fixture", failure.Tool)
	require.Equal(t, "sub", failure.Command)
	require.Equal(t, "err", failure.Stderr)
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, "the exit status stays reachable")
	require.Equal(t, 3, exit.ExitCode())
	require.Equal(t, "fixture sub: exit status 3: err", err.Error())
	_, err = Run(t.Context(), Spec{Tool: "fixture", Command: "named", Path: path, Args: []string{"-q", "sub"}})
	require.ErrorContains(t, err, "fixture named:")

	combined, err := Run(t.Context(), Spec{Tool: "fixture", Path: path, Args: []string{"sub"}, Combined: true})
	require.Error(t, err)
	require.Contains(t, string(combined.Output), "out")
	require.Contains(t, string(combined.Output), "err")
	require.Empty(t, combined.Stderr)
	require.ErrorContains(t, err, "fixture sub: exit status 3: out\nerr")

	// Nothing on stderr leaves no ": " behind the exit status.
	_, err = Run(t.Context(), Spec{Tool: "fixture", Path: script(t, "exit 36\n"), Args: []string{"find"}})
	require.EqualError(t, err, "fixture find: exit status 36")
}

func TestRunStreamsToAnExtraSinkAndHonorsInputDirAndEnv(t *testing.T) {
	path := script(t, "cat; pwd; echo \"$FIXTURE_VAR\"\n")
	var sink strings.Builder
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	result, err := Run(t.Context(), Spec{Tool: "fixture", Path: path, Dir: dir, Env: []string{"FIXTURE_VAR=set", "PATH=" + os.Getenv("PATH")}, Stdin: strings.NewReader("typed\n"), Stdout: &sink})
	require.NoError(t, err)
	require.Equal(t, "typed\n"+resolved+"\nset\n", string(result.Output))
	require.Equal(t, string(result.Output), sink.String(), "the sink sees everything the capture sees")
}

func TestRunBoundsOutputAndJoinsCancellation(t *testing.T) {
	path := script(t, "yes | head -c 4096\n")
	_, err := Run(t.Context(), Spec{Tool: "fixture", Path: path, Limit: 512})
	require.ErrorIs(t, err, errOutputLimit)

	slow := script(t, "sleep 5\n")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = Run(ctx, Spec{Tool: "fixture", Path: slow, WaitDelay: 100 * time.Millisecond})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, errors.Is(err, errOutputLimit))
	_, err = Run(t.Context(), Spec{Tool: "fixture"})
	require.ErrorContains(t, err, "executable is required")
}

// Draining, output past the limit is dropped and the command runs to its
// end: its exit status, not its output's length, decides.
func TestRunDrainsOutputPastTheLimit(t *testing.T) {
	path := script(t, "yes | head -c 4096; echo done >&2; exit \"$FIXTURE_EXIT\"\n")
	result, err := Run(t.Context(), Spec{Tool: "fixture", Path: path, Limit: 512, Drain: true, Env: []string{"FIXTURE_EXIT=0", "PATH=" + os.Getenv("PATH")}})
	require.NoError(t, err)
	require.True(t, result.Truncated)
	require.Len(t, result.Output, 512)
	require.Equal(t, "done\n", string(result.Stderr), "the command ran to its end")

	result, err = Run(t.Context(), Spec{Tool: "fixture", Path: path, Limit: 512, Drain: true, Env: []string{"FIXTURE_EXIT=4", "PATH=" + os.Getenv("PATH")}})
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 4, exit.ExitCode())
	require.NotErrorIs(t, err, errOutputLimit, "the overflow isn't the failure")
	require.True(t, result.Truncated)
}
