package port

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/testenv"
)

// portdirWith writes a Portfile into a fresh directory.
func portdirWith(t *testing.T, portfile string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))
	return dir
}

func TestDerivationsCopy(t *testing.T) {
	h := New(tree.Target{Portdir: "/tree/sysutils/foo", Subport: "foo-sub"}, nil)

	at := h.At("/tmp/shadow")
	sub := h.Subport("other-sub")
	withVars := h.WithVariants(info.VariantSet("+x11"))

	// Each derivation changes one field and leaves the original alone.
	assert.Equal(t, "/tmp/shadow", at.Target.Portdir)
	assert.Equal(t, "foo-sub", at.Target.Subport, "At keeps the context")
	assert.Equal(t, "other-sub", sub.Target.Subport)
	assert.Equal(t, "/tree/sysutils/foo", sub.Target.Portdir, "Subport keeps the portdir")
	assert.Equal(t, info.VariantSet("+x11"), withVars.Variants)

	assert.Equal(t, "/tree/sysutils/foo", h.Target.Portdir, "original untouched")
	assert.Equal(t, "foo-sub", h.Target.Subport)
	assert.Empty(t, h.Variants)
}

func TestSource(t *testing.T) {
	dir := portdirWith(t, "PortSystem 1.0\nname foo\nversion 1.2.3\n")
	src, cst, err := New(tree.Target{Portdir: dir}, nil).Source()
	require.NoError(t, err)
	assert.Contains(t, string(src), "version 1.2.3")
	require.NotNil(t, cst)
	assert.Len(t, cst.Items, 3)

	// An unresolved target has no Portfile to read.
	_, _, err = New(tree.Target{}, nil).Source()
	require.ErrorIs(t, err, tree.ErrNoPortdir)

	// A Portfile that does not parse is an error, not a partial tree.
	bad := portdirWith(t, "version {unterminated\n")
	_, _, err = New(tree.Target{Portdir: bad}, nil).Source()
	require.Error(t, err)
}

// evaluator starts a real evaluator, skipping without port-tclsh.
func evaluator(t *testing.T) *eval.Evaluator {
	t.Helper()
	proc, err := shell.Start(context.Background(), testenv.PortTclsh(t))
	require.NoError(t, err)
	ev, err := eval.New(context.Background(), proc)
	require.NoError(t, err)
	t.Cleanup(func() { ev.Close() })
	return ev
}

func TestValuesAcrossContexts(t *testing.T) {
	ev := evaluator(t)
	ctx := context.Background()
	dir := portdirWith(t, `PortSystem 1.0
name             probe
version          1.0
categories       devel
maintainers      nomaintainer
license          MIT
description      probe
long_description probe
subport probe-sub { version 2.0 }
`)
	h := New(tree.Target{Portdir: dir}, ev)

	// The bound context: the top-level port.
	vals, err := h.Values(ctx)
	require.NoError(t, err)
	assert.Equal(t, "probe", vals.Name)
	assert.Equal(t, "1.0", vals.Version)

	// A derived handle addresses the sibling context, in one evaluation.
	subVals, err := h.Subport("probe-sub").Values(ctx)
	require.NoError(t, err)
	assert.Equal(t, "probe-sub", subVals.Name)
	assert.Equal(t, "2.0", subVals.Version)

	names, err := h.SubportNames(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"probe-sub"}, names)

	// Totality: the snapshot carries every context.
	snap, err := h.Snapshot(ctx)
	require.NoError(t, err)
	assert.Len(t, snap, 2)

	// A subport the Portfile does not define is MacPorts' own error,
	// which is why Subport does not validate.
	_, err = h.Subport("probe-nope").Values(ctx)
	require.ErrorContains(t, err, "does not have a subport")
}

func TestAtEvaluatesElsewhere(t *testing.T) {
	ev := evaluator(t)
	base := portdirWith(t, "PortSystem 1.0\nname probe\nversion 1.0\n")
	other := portdirWith(t, "PortSystem 1.0\nname probe\nversion 9.9\n")

	h := New(tree.Target{Portdir: base}, ev)
	vals, err := h.At(other).Values(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "9.9", vals.Version, "At redirects evaluation, as shadowing needs")
}

func TestShadow(t *testing.T) {
	portdir := portdirWith(t, "PortSystem 1.0\nname foo\nversion 1.0\n")
	require.NoError(t, os.MkdirAll(filepath.Join(portdir, "files"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "files", "patch-x.diff"), []byte("--- a\n+++ b\n"), 0o644))
	// A work symlink from a local build is not part of the port.
	require.NoError(t, os.Symlink("/nonexistent/build", filepath.Join(portdir, "work")))

	h := New(tree.Target{Portdir: portdir, Subport: "foo-sub"}, nil)
	sh, cleanup, err := h.Shadow([]byte("PortSystem 1.0\nname foo\nversion 2.0\n"))
	require.NoError(t, err)

	// The shadow is a handle on the copy, carrying the same context.
	assert.NotEqual(t, portdir, sh.Target.Portdir)
	assert.Equal(t, "foo-sub", sh.Target.Subport)

	pf, err := os.ReadFile(filepath.Join(sh.Target.Portdir, macports.PortfileName))
	require.NoError(t, err)
	assert.Equal(t, "PortSystem 1.0\nname foo\nversion 2.0\n", string(pf), "Portfile carries the replacement")

	patch, err := os.ReadFile(filepath.Join(sh.Target.Portdir, "files", "patch-x.diff"))
	require.NoError(t, err)
	assert.Equal(t, "--- a\n+++ b\n", string(patch), "files/ rides along")

	_, err = os.Lstat(filepath.Join(sh.Target.Portdir, "work"))
	assert.True(t, os.IsNotExist(err), "symlinks are skipped")

	// Cleanup removes the copy and leaves the original alone; calling
	// it twice is harmless.
	cleanup()
	cleanup()
	_, err = os.Stat(sh.Target.Portdir)
	assert.True(t, os.IsNotExist(err), "shadow removed")
	_, err = os.Stat(filepath.Join(portdir, macports.PortfileName))
	assert.NoError(t, err, "the real portdir is untouched")
}

// The shadow of devel/foo lives at <tmp>/devel/foo: evaluation never
// cares, but the verifier's overlay stages a portdir by its layout, and
// a shadow that discarded the category could not be staged as the port
// it shadows.
func TestShadowKeepsTheCategoryLayout(t *testing.T) {
	base := t.TempDir()
	portdir := filepath.Join(base, "devel", "foo")
	require.NoError(t, os.MkdirAll(portdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, macports.PortfileName),
		[]byte("PortSystem 1.0\nname foo\nversion 1.0\n"), 0o644))

	h := New(tree.Target{Portdir: portdir}, nil)
	sh, cleanup, err := h.Shadow([]byte("PortSystem 1.0\nname foo\nversion 2.0\n"))
	require.NoError(t, err)
	defer cleanup()

	assert.Equal(t, "foo", filepath.Base(sh.Target.Portdir))
	assert.Equal(t, "devel", filepath.Base(filepath.Dir(sh.Target.Portdir)),
		"the category survives into the shadow's path")
}
