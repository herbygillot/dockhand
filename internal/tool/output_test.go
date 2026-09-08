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
	res, err := Output(t.Context(), "/bin/sh", sh("echo data; echo noise >&2"))
	require.NoError(t, err)
	assert.Equal(t, 0, res.Code)
	assert.Equal(t, "data\n", string(res.Stdout), "stdout alone; stderr is not data")
	assert.Equal(t, "noise", res.Stderr, "trimmed, because it is a message")
	assert.False(t, res.Truncated)
}

func TestOutputWordsAFailureWithItsStderr(t *testing.T) {
	res, err := Output(t.Context(), "/bin/sh", sh("echo partial; echo ' oops ' >&2; exit 3"))
	require.Error(t, err)
	assert.Equal(t, 3, res.Code)
	assert.Equal(t, "oops", err.Error(), "the trimmed stderr is the message a wrapper prefixes")

	var f *Failure
	require.ErrorAs(t, err, &f)
	assert.Equal(t, 3, f.Code)
	assert.Equal(t, "oops", f.Stderr)
	var ee *exec.ExitError
	assert.ErrorAs(t, f.Err, &ee, "the exec error is kept as it came")
}

// D23, and the reason D5 was possible at all. A child that exits
// non-zero has usually already said the thing its caller needs —
// `gh api --include` prints "HTTP/2.0 304 Not Modified" and then exits
// one, because a 304 is not a 2xx — and a transport that dropped those
// bytes left the status recoverable only from the wrapper's error
// prose.
func TestOutputKeepsStdoutWhenTheCommandFails(t *testing.T) {
	res, err := Output(t.Context(), "/bin/sh", sh("echo 'HTTP/2.0 304 Not Modified'; echo boom >&2; exit 1"))
	require.Error(t, err, "a non-zero exit is still a failure")
	assert.Equal(t, "HTTP/2.0 304 Not Modified\n", string(res.Stdout),
		"what the child wrote is evidence; how it exited is a separate fact")
	assert.Equal(t, "boom", res.Stderr)
	assert.Equal(t, 1, res.Code)
}

func TestOutputFallsBackToTheExecErrorWhenStderrIsEmpty(t *testing.T) {
	res, err := Output(t.Context(), "/bin/sh", sh("exit 2"))
	require.Error(t, err)
	assert.Equal(t, 2, res.Code)
	var f *Failure
	require.ErrorAs(t, err, &f)
	assert.Empty(t, f.Stderr)
	assert.Equal(t, f.Err.Error(), err.Error(), "with nothing on stderr, os/exec's own words are the message")
}

func TestOutputReportsAStartFailure(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent")
	res, err := Output(t.Context(), absent, Opts{})
	require.Error(t, err)
	assert.Empty(t, res.Stdout)
	assert.Equal(t, -1, res.Code, "a process that never ran has no exit status")
	var f *Failure
	require.ErrorAs(t, err, &f)
	assert.Equal(t, -1, f.Code)
	assert.Equal(t, f.Err.Error(), err.Error())
}

func TestOutputUnwrapsToTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	res, err := Output(ctx, "/bin/sh", sh("sleep 5"))
	require.ErrorIs(t, err, context.Canceled, "a Failure unwraps to what exec reported")
	assert.Equal(t, -1, res.Code)
}

// The bound keeps a runaway child out of memory and out of an error
// message, and says so rather than handing back a fragment that looks
// like an answer. It bounds each stream separately, and it must not
// turn a verbose command into a failed one.
func TestOutputLimitBoundsEachStreamAndSaysSo(t *testing.T) {
	res, err := Output(t.Context(), "/bin/sh", Opts{
		Args:  []string{"-c", "printf 0123456789; printf abcdefghij >&2"},
		Limit: 4,
	})
	require.NoError(t, err, "a command that talked too much still succeeded")
	assert.Equal(t, "0123", string(res.Stdout))
	assert.Equal(t, "abcd", res.Stderr)
	assert.True(t, res.Truncated, "a fragment must not be mistaken for the whole")

	res, err = Output(t.Context(), "/bin/sh", Opts{Args: []string{"-c", "printf 0123"}, Limit: 4})
	require.NoError(t, err)
	assert.Equal(t, "0123", string(res.Stdout))
	assert.False(t, res.Truncated, "exactly the bound is not past it")

	res, err = Output(t.Context(), "/bin/sh", sh("printf 0123456789"))
	require.NoError(t, err)
	assert.Equal(t, "0123456789", string(res.Stdout), "the zero bound keeps everything")
	assert.False(t, res.Truncated)
}

func TestOutputFeedsStdin(t *testing.T) {
	o := sh("cat")
	o.Stdin = strings.NewReader("fed")
	res, err := Output(t.Context(), "/bin/sh", o)
	require.NoError(t, err)
	assert.Equal(t, "fed", string(res.Stdout))
}

func TestOutputEnvironment(t *testing.T) {
	t.Setenv("DOCKHAND_TOOL_PROBE", "inherited")
	script := sh(`printf %s "$DOCKHAND_TOOL_PROBE"`)

	res, err := Output(t.Context(), "/bin/sh", script)
	require.NoError(t, err)
	assert.Equal(t, "inherited", string(res.Stdout), "a nil Env inherits the process environment")

	script.Env = []string{"DOCKHAND_TOOL_PROBE=given"}
	res, err = Output(t.Context(), "/bin/sh", script)
	require.NoError(t, err)
	assert.Equal(t, "given", string(res.Stdout), "a given Env is the whole environment")
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
