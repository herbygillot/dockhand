package tool

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sh runs a shell script through Output; every case here is a script
// because /bin/sh is the one tool a hermetic runner is sure to have.
func sh(script string) Opts { return Opts{Args: []string{"-c", script}} }

func TestOutputReturnsStdoutOnSuccess(t *testing.T) {
	out, code, err := Output(t.Context(), "/bin/sh", sh("echo data; echo noise >&2"))
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "data\n", string(out), "stdout alone; stderr is not data")
}

func TestOutputWordsAFailureWithItsStderr(t *testing.T) {
	out, code, err := Output(t.Context(), "/bin/sh", sh("echo partial; echo ' oops ' >&2; exit 3"))
	require.Error(t, err)
	assert.Nil(t, out, "a failed command's stdout is dropped, as every wrapper drops it")
	assert.Equal(t, 3, code)
	assert.Equal(t, "oops", err.Error(), "the trimmed stderr is the message a wrapper prefixes")

	var f *Failure
	require.ErrorAs(t, err, &f)
	assert.Equal(t, 3, f.Code)
	assert.Equal(t, "oops", f.Stderr)
	var ee *exec.ExitError
	assert.ErrorAs(t, f.Err, &ee, "the exec error is kept as it came")
}

func TestOutputFallsBackToTheExecErrorWhenStderrIsEmpty(t *testing.T) {
	_, code, err := Output(t.Context(), "/bin/sh", sh("exit 2"))
	require.Error(t, err)
	assert.Equal(t, 2, code)
	var f *Failure
	require.ErrorAs(t, err, &f)
	assert.Empty(t, f.Stderr)
	assert.Equal(t, f.Err.Error(), err.Error(), "with nothing on stderr, os/exec's own words are the message")
}

func TestOutputReportsAStartFailure(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent")
	out, code, err := Output(t.Context(), absent, Opts{})
	require.Error(t, err)
	assert.Nil(t, out)
	assert.Equal(t, -1, code, "a process that never ran has no exit status")
	var f *Failure
	require.ErrorAs(t, err, &f)
	assert.Equal(t, -1, f.Code)
	assert.Equal(t, f.Err.Error(), err.Error())
}

func TestOutputUnwrapsToTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, code, err := Output(ctx, "/bin/sh", sh("sleep 5"))
	require.ErrorIs(t, err, context.Canceled, "a Failure unwraps to what exec reported")
	assert.Equal(t, -1, code)
}

func TestOutputFeedsStdin(t *testing.T) {
	o := sh("cat")
	o.Stdin = strings.NewReader("fed")
	out, _, err := Output(t.Context(), "/bin/sh", o)
	require.NoError(t, err)
	assert.Equal(t, "fed", string(out))
}

func TestOutputEnvironment(t *testing.T) {
	t.Setenv("DOCKHAND_TOOL_PROBE", "inherited")
	script := sh(`printf %s "$DOCKHAND_TOOL_PROBE"`)

	out, _, err := Output(t.Context(), "/bin/sh", script)
	require.NoError(t, err)
	assert.Equal(t, "inherited", string(out), "a nil Env inherits the process environment")

	script.Env = []string{"DOCKHAND_TOOL_PROBE=given"}
	out, _, err = Output(t.Context(), "/bin/sh", script)
	require.NoError(t, err)
	assert.Equal(t, "given", string(out), "a given Env is the whole environment")
}

func TestRunMergesTheStreamsAndKeepsTheExecError(t *testing.T) {
	out, err := Run(t.Context(), "/bin/sh", sh("echo out; echo err >&2; exit 4"))
	assert.Equal(t, "out\nerr\n", out, "both streams, in order, returned even on failure")
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee, "the exec error as it came, unworded")
	assert.Equal(t, 4, ee.ExitCode())

	out, err = Run(t.Context(), "/bin/sh", sh("echo fine"))
	require.NoError(t, err)
	assert.Equal(t, "fine\n", out)
}

func TestRunFeedsStdin(t *testing.T) {
	o := sh("cat")
	o.Stdin = strings.NewReader("piped")
	out, err := Run(t.Context(), "/bin/sh", o)
	require.NoError(t, err)
	assert.Equal(t, "piped", out)
}
