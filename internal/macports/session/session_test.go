package session

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/shim"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func TestRootGuard(t *testing.T) {
	require.ErrorIs(t, rootGuard(0, false), ErrRootRefused)
	require.NoError(t, rootGuard(0, true))
	require.NoError(t, rootGuard(501, false))
}

// embeddedVersions lists every shim in the set, by the version it is
// named for.
func embeddedVersions(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(shimFS, shimDir)
	require.NoError(t, err)
	var versions []string
	for _, e := range entries {
		if v, ok := strings.CutSuffix(e.Name(), ".tcl"); ok {
			versions = append(versions, v)
		}
	}
	require.NotEmpty(t, versions)
	return versions
}

// The "one shim set" gate: whichever shim Select lands on, it registers
// the evaluator's ops and the fetcher's ops alike, and initializes
// MacPorts exactly once for both.
func TestShimSetServesBothHalves(t *testing.T) {
	ops := []string{"snapshot", "fetchinfo", "options", "fetchdist", "livecheckrun", "vercmp"}
	for _, v := range embeddedVersions(t) {
		script, named, err := shim.Select(shimFS, shimDir, v)
		require.NoError(t, err, v)
		assert.Equal(t, v, named)
		for _, op := range ops {
			assert.Contains(t, script, "\n::tclrpc::register "+op+" ", "shim %s does not register %s", v, op)
		}
		assert.Equal(t, 1, strings.Count(script, "\nmportinit\n"), "shim %s must run mportinit exactly once", v)
		assert.Equal(t, 1, strings.Count(script, "package require macports"), "shim %s must load macports exactly once", v)
	}
}

func TestNewestShimIsTheEmbeddedSet(t *testing.T) {
	got, err := NewestShim()
	require.NoError(t, err)
	require.NotEmpty(t, got)
	want, err := shim.Newest(shimFS, "shims")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// A prefix with no port-tclsh under it fails at the shell start, and
// the failure carries the startup identity the exit table classifies
// on. WithVersion("") keeps Start from probing the missing port client
// first — undetermined is an answer, not a request for a probe.
func TestStartCarriesErrStartupWithoutAPrefix(t *testing.T) {
	_, err := Start(context.Background(), prefix.Prefix("/nonexistent"), WithVersion(""))
	require.ErrorIs(t, err, ErrStartup)
}

func TestWithVersionRecordsAStatedEmptyVersion(t *testing.T) {
	var cfg config
	WithVersion("")(&cfg)
	assert.True(t, cfg.versionSet, "an empty stated version is still stated")
	assert.Empty(t, cfg.version)
}

// One session answers both halves: the fetcher's vercmp and the
// evaluator's snapshot, over the same port-tclsh.
func TestStartServesBothHalvesInOneSession(t *testing.T) {
	pfx := testenv.MacPortsPrefix(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s, err := Start(ctx, pfx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	assert.NotEmpty(t, s.Shim())

	reply, err := s.Call(ctx, "vercmp", "1.0", "2.0")
	require.NoError(t, err)
	assert.Equal(t, "-1", reply)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName),
		[]byte("PortSystem 1.0\nname sessiondemo\nversion 1.0\n"), 0o644))
	reply, err = s.Call(ctx, "snapshot", dir)
	require.NoError(t, err)
	fields, errs := syntax.DictValues(reply)
	require.Empty(t, errs, "snapshot reply %q is not a dict", reply)
	assert.Equal(t, "sessiondemo", fields["name"])
	assert.Equal(t, "1.0", fields["version"])
}
