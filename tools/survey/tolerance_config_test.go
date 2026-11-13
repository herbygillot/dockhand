package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMeasurementIdentityPinsBaseSourcesAndSelection(t *testing.T) {
	prefix := t.TempDir()
	file := filepath.Join(prefix, "libexec/macports/lib/port1.0/fixture.tcl")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
	require.NoError(t, os.WriteFile(file, []byte("default x 1\n"), 0o600))
	a, err := measurementIdentity("ports a", prefix)
	require.NoError(t, err)
	require.NotEmpty(t, a.BinarySHA)
	b, err := measurementIdentity("ports b", prefix)
	require.NoError(t, err)
	require.NotEqual(t, a.key(), b.key())
	require.NoError(t, os.WriteFile(file, []byte("default x 2\n"), 0o600))
	c, err := measurementIdentity("ports a", prefix)
	require.NoError(t, err)
	require.NotEqual(t, a.BaseSources, c.BaseSources)
}
