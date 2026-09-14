package tart

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientAppliesTartEnvironmentAndStreamsOutput(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "tart")
	require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\nprintf '%s\\n' \"$TART_HOME:$TART_NO_AUTO_PRUNE:$LC_ALL:$1\"\n"), 0700))
	var streamed bytes.Buffer
	output, err := (Client{Executable: executable, Home: "/tart/home"}).Run(t.Context(), RunOptions{Output: &streamed}, "list")
	require.NoError(t, err)
	require.Equal(t, "/tart/home:1:C:list\n", string(output))
	require.Equal(t, output, streamed.Bytes())
}

func TestClientCombinedOutputRetainsCommandFailure(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "tart")
	require.NoError(t, os.WriteFile(executable, []byte("#!/bin/sh\necho stdout\necho stderr >&2\nexit 7\n"), 0700))
	output, err := (Client{Executable: executable, Home: t.TempDir()}).Run(t.Context(), RunOptions{Combined: true}, "clone")
	require.ErrorContains(t, err, "tart: clone")
	require.Contains(t, string(output), "stdout")
	require.Contains(t, string(output), "stderr")
}
