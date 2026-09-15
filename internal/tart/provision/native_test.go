package provision

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentReadinessReportsCancellationAndLastProbe(t *testing.T) {
	script := filepath.Join(t.TempDir(), "tart")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho 'fixture agent unavailable' >&2\nexit 1\n"), 0700))
	n := newNative(Config{Executable: script}, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	err := n.ReadyAgent(ctx, "candidate")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "last probe")
}

func TestAgentReadinessDetectsVMExit(t *testing.T) {
	script := filepath.Join(t.TempDir(), "tart")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0700))
	n := newNative(Config{Executable: script}, nil)
	done := make(chan error, 1)
	failure := errors.New("fixture VM exited")
	done <- failure
	n.runs["candidate"] = done
	require.ErrorIs(t, n.ReadyAgent(t.Context(), "candidate"), failure)
}

func TestProvisioningGuestTransportPreservesStreamingAndFailure(t *testing.T) {
	script := filepath.Join(t.TempDir(), "tart")
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
set -eu
[ "$1" = exec ]
[ "$2" = -i ]
shift 3
exec 9>/dev/null
exec "$@"
`), 0700))
	var progress bytes.Buffer
	n := newNative(Config{Executable: script}, &progress)
	output, err := n.guestStream(t.Context(), "candidate", strings.NewReader("payload\n"), "/bin/sh", "-c", `
[ ! -e /dev/fd/9 ] || exit 42
cat
printf 'diagnostic\n' >&2
exit 7
`)
	require.ErrorContains(t, err, "exit status 7")
	require.Equal(t, "payload\ndiagnostic\n", string(output))
	require.Contains(t, progress.String(), "payload")
	require.Contains(t, progress.String(), "diagnostic")
}
