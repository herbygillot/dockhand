package tart

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func TestClientAppliesTartEnvironmentAndStreamsOutput(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, executable, "#!/bin/sh\nprintf '%s\\n' \"$TART_HOME:$TART_NO_AUTO_PRUNE:$LC_ALL:$1\"\n")
	var streamed bytes.Buffer
	output, err := (Client{Executable: executable, Home: "/tart/home"}).Run(t.Context(), RunOptions{Output: &streamed}, "list")
	require.NoError(t, err)
	require.Equal(t, "/tart/home:1:C:list\n", string(output))
	require.Equal(t, output, streamed.Bytes())
}

func TestClientCombinedOutputRetainsCommandFailure(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "tart")
	testsupport.WriteExecutable(t, executable, "#!/bin/sh\necho stdout\necho stderr >&2\nexit 7\n")
	output, err := (Client{Executable: executable, Home: t.TempDir()}).Run(t.Context(), RunOptions{Combined: true}, "clone")
	require.ErrorContains(t, err, "tart clone")
	require.Contains(t, string(output), "stdout")
	require.Contains(t, string(output), "stderr")
}
