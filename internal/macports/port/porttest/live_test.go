package porttest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// The live helpers are the other half of the package, and they are
// gated: this skips without port-tclsh, exactly as the hand-copied
// versions it replaced did. What it proves is that the consolidated
// helper starts the same evaluator those copies started — a real one,
// against the prefix the discovered port-tclsh sits in — because the
// value of consolidating them is nil if the shared one is subtly
// different.
func TestLiveHandleEvaluatesARealPortfile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(`PortSystem 1.0
name             probe
version          1.0
categories       devel
maintainers      nomaintainer
license          MIT
description      probe
long_description probe
`), 0o644))

	h := LiveHandle(t, dir)
	vals, err := h.Values(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "probe", vals.Name)
	assert.Equal(t, "1.0", vals.Version)

	// Totality is the property a snapshot carries, so it is asked for
	// here rather than assumed from Values.
	snap, err := h.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Len(t, snap, 1)
}

func TestFetcherStarts(t *testing.T) {
	assert.NotNil(t, Fetcher(t))
}
