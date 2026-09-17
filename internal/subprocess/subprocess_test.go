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

	"github.com/stretchr/testify/require"
)

func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tool")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700))
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
