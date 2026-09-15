package provision

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
