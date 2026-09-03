package porttest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/info"
)

func key(subport string) info.SubportKey { return info.SubportKey{Subport: subport} }

// The sequence a planner's tail runs: snapshot the portdir, shadow the
// edited source, snapshot the shadow, diff. Every step here is a real
// Handle doing what it really does — the shadow is a real directory
// copy — and the only thing standing in is what MacPorts would have
// said. That is the split the package exists for, so it is the thing
// proved first.
func TestAScriptedOracleDrivesTheTailWithoutATclShell(t *testing.T) {
	portdir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(portdir, macports.PortfileName),
		[]byte("PortSystem 1.0\nname foo\nversion 1.0\n"), 0o644))

	before := info.Snapshot{key("foo"): {Name: "foo", Version: "1.0"}}
	after := info.Snapshot{key("foo"): {Name: "foo", Version: "2.0"}}
	h := Handle(Shadowed(portdir, before, after), portdir)

	got, err := h.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, before, got)

	shadow, cleanup, err := h.Shadow([]byte("PortSystem 1.0\nname foo\nversion 2.0\n"))
	require.NoError(t, err)
	defer cleanup()
	require.NotEqual(t, portdir, shadow.Target.Portdir, "the shadow is elsewhere, which is how it is recognized")

	now, err := shadow.Snapshot(t.Context())
	require.NoError(t, err)

	delta := got.Diff(now)
	require.Empty(t, delta.Added)
	require.Empty(t, delta.Removed)
	changes := delta.Changed[key("foo")]
	require.Len(t, changes, 1)
	assert.Equal(t, info.FieldVersion, changes[0].Field)
	assert.Equal(t, []string{"2.0"}, changes[0].New)
}

// A question the test did not script is an error naming the question,
// never a zero value. An empty snapshot would read as a Portfile that
// defines no context at all, and a planner handed that declines for a
// reason nobody wrote.
func TestAnUnscriptedQuestionSaysSo(t *testing.T) {
	h := Handle(&Oracle{}, "/tree/devel/foo")

	_, err := h.Snapshot(t.Context())
	require.ErrorIs(t, err, ErrUnscripted)
	require.ErrorContains(t, err, "Snapshot")
	require.ErrorContains(t, err, "/tree/devel/foo")

	_, err = h.Subport("foo-sub").Values(t.Context())
	require.ErrorIs(t, err, ErrUnscripted)
	require.ErrorContains(t, err, "foo-sub", "the context is named, since a planner asks about several")

	for _, ask := range []func() error{
		func() error { _, err := h.SubportNames(t.Context()); return err },
		func() error { _, err := h.Options(t.Context(), "distname"); return err },
		func() error { _, err := h.FetchInfo(t.Context(), true); return err },
	} {
		assert.ErrorIs(t, ask(), ErrUnscripted)
	}
}

// Every question is answerable, and each is handed the frame the handle
// carries — otherwise a scripted test could not tell a variant-aware
// planner from one that ignores the frame.
func TestTheOracleForwardsTheHandlesFrame(t *testing.T) {
	var sawVariants info.VariantSet
	var sawSubport string
	o := &Oracle{
		OnValues: func(_ context.Context, _, subport string, v info.VariantSet) (info.Values, error) {
			sawSubport, sawVariants = subport, v
			return info.Values{Name: subport}, nil
		},
	}
	h := Handle(o, "/tree/devel/foo").Subport("foo-sub").WithVariants(info.VariantSet("+x11"))
	vals, err := h.Values(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "foo-sub", vals.Name)
	assert.Equal(t, "foo-sub", sawSubport)
	assert.Equal(t, info.VariantSet("+x11"), sawVariants)
}
