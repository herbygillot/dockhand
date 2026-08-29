package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func newEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	path := testenv.PortTclsh(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	proc, err := shell.Start(ctx, path)
	require.NoError(t, err)
	e, err := New(ctx, proc)
	require.NoError(t, err) // New kills the proc on failure
	t.Cleanup(func() { e.Close() })
	return e
}

func portdirWith(t *testing.T, portfile string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"), []byte(portfile), 0o644))
	return dir
}

func TestSnapshotMinimalPort(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
name                rung1demo
version             1.2.3
revision            7
epoch               2
categories          devel test
license             MIT
maintainers         {gmail.com:herby.gillot @herbygillot} openmaintainer
platforms           darwin
checksums           rmd160 abc sha256 def
depends_build       port:pkgconfig
depends_lib         port:zlib port:openssl
`)
	snap, err := e.Snapshot(context.Background(), dir, "")
	require.NoError(t, err)
	v, ok := snap[info.SubportKey{Subport: "rung1demo"}]
	require.True(t, ok, "no entry for port; snapshot = %v", snap)
	require.Equal(t, "1.2.3", v.Version)
	require.Equal(t, "7", v.Revision)
	require.Equal(t, "2", v.Epoch)
	require.Equal(t, []string{"devel", "test"}, v.Categories)
	require.Equal(t, []string{"MIT"}, v.License)
	require.Equal(t, []string{"gmail.com:herby.gillot @herbygillot", "openmaintainer"}, v.Maintainers)
	require.Equal(t, []string{"darwin"}, v.Platforms)
	require.Equal(t, []string{"rmd160", "abc", "sha256", "def"}, v.Checksums)
	require.Equal(t, []string{"rung1demo-1.2.3.tar.gz"}, v.Distfiles,
		"default distfiles derives from name and version")
	require.Equal(t, []string{"port:pkgconfig"}, v.Depends.Build)
	require.Equal(t, []string{"port:zlib", "port:openssl"}, v.Depends.Lib)
}

// TestSnapshotComputedVersion is the risk probe rung 1 exists for: a real
// fixture whose version is a positional argument to a PortGroup command.
// Evaluating it requires the golang and github PortGroups to resolve for an
// out-of-tree portdir, and yields a version no textual read could produce
// as reliably.
func TestSnapshotComputedVersion(t *testing.T) {
	e := newEvaluator(t)
	src, err := os.ReadFile("../testdata/portfiles/math__ivy")
	require.NoError(t, err)
	dir := portdirWith(t, string(src))
	snap, err := e.Snapshot(context.Background(), dir, "")
	require.NoError(t, err)
	v, ok := snap[info.SubportKey{Subport: "ivy"}]
	require.True(t, ok, "no entry for ivy; snapshot = %v", snap)
	require.NotEmpty(t, v.Version, "portgroup-carried version came back empty: %+v", v)
	t.Logf("ivy evaluated: version=%s revision=%s categories=%v", v.Version, v.Revision, v.Categories)
}

func TestSnapshotMissingPortdir(t *testing.T) {
	e := newEvaluator(t)
	_, err := e.Snapshot(context.Background(), filepath.Join(t.TempDir(), "nope"), "")
	require.Error(t, err, "snapshot of missing portdir must fail")
}

func TestRootGuard(t *testing.T) {
	require.ErrorIs(t, rootGuard(0, false), ErrRootRefused)
	require.NoError(t, rootGuard(0, true))
	require.NoError(t, rootGuard(501, false))
}
