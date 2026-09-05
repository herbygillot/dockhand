package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func newEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e, err := Start(ctx, testenv.MacPortsPrefix(t))
	require.NoError(t, err)
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
	dir := testenv.PortfileDir(t, "math__ivy")
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

func TestValuesHydratesProseAndConfig(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
name             hydrate
version          1.0
categories       devel
maintainers      nomaintainer
license          MIT
description      A one-line summary with spaces
long_description This is the longer prose a port states about itself.
homepage         https://example.org/hydrate
livecheck.type   regex
livecheck.url    https://example.org/tags
livecheck.regex  {tags/([0-9.]+)\.tar\.gz}
`)
	v, err := e.Values(context.Background(), dir, "", "")
	require.NoError(t, err)

	// Prose arrives unbraced, spaces intact.
	require.Equal(t, "A one-line summary with spaces", v.Description)
	require.Equal(t, "This is the longer prose a port states about itself.", v.LongDescription)
	require.Equal(t, "https://example.org/hydrate", v.Homepage)

	// Configuration rides along in the same evaluation.
	require.Equal(t, "regex", v.Livecheck.Type)
	require.Equal(t, "https://example.org/tags", v.Livecheck.URL)
	require.Equal(t, `tags/([0-9.]+)\.tar\.gz`, v.Livecheck.Regex)
	require.Equal(t, "1.0", v.Livecheck.Version)
	require.False(t, v.Vendored.Any())
	// A port that says nothing about its patch phase patches at base's
	// default, and the shim reports the option rather than its absence:
	// the decoder's own default is base's text, so a reply with the key
	// and one without read the same.
	require.Equal(t, DefaultPatchPreArgs, v.PatchPreArgs)
	require.Equal(t, 0, StripLevel(v.PatchPreArgs))
}

// The patch phase's arguments are the port's own when it sets them,
// read off the worker the same way every other option is.
func TestValuesReportsPatchPreArgs(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
name             stripped
version          1.0
categories       devel
maintainers      nomaintainer
license          MIT
description      patch strip probe
long_description patch strip probe
patchfiles       patch-foo.diff
patch.pre_args   -p1
`)
	v, err := e.Values(context.Background(), dir, "", "")
	require.NoError(t, err)
	require.Equal(t, []string{"patch-foo.diff"}, v.Patchfiles)
	require.Equal(t, "-p1", v.PatchPreArgs)
	require.Equal(t, 1, StripLevel(v.PatchPreArgs))
}

func TestValuesReportsVendoredBlocks(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
PortGroup        golang 1.0
go.setup         github.com/example/thing 1.0
categories       devel
maintainers      nomaintainer
license          MIT
description      vendored probe
long_description vendored probe
go.vendors       golang.org/x/sys \
                     lock    v0.1.0 \
                     rmd160  0 \
                     sha256  0 \
                     size    1
`)
	v, err := e.Values(context.Background(), dir, "", "")
	require.NoError(t, err)
	require.True(t, v.Vendored.Any())
	require.Contains(t, v.Vendored.GoVendors, "golang.org/x/sys")
	require.Empty(t, v.Vendored.CargoCrates)
}
