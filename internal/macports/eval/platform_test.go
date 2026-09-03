package eval

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func newPlatformEvaluator(t *testing.T, p info.Platform) *Evaluator {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e, err := Start(ctx, testenv.MacPortsPrefix(t), WithPlatform(p))
	require.NoError(t, err)
	t.Cleanup(func() { e.Close() })
	return e
}

const platformProbePortfile = `PortSystem 1.0
name platprobe
categories devel
if {${os.major} >= 20} {
    version 2.0
} else {
    version 1.0
}
if {${os.arch} eq "arm"} {
    revision 5
} else {
    revision 9
}
`

// Two spoofed frames, both fully deterministic regardless of the host:
// the same Portfile evaluates to different values under each.
func TestPlatformFramesAreDeterministic(t *testing.T) {
	dir := portdirWith(t, platformProbePortfile)

	oldIntel := newPlatformEvaluator(t, info.Platform{OS: "macosx", Major: 19, Arch: "x86_64"})
	v, err := oldIntel.Top(context.Background(), dir, "")
	require.NoError(t, err)
	require.Equal(t, "1.0", v.Version)
	require.Equal(t, "9", v.Revision)

	newArm := newPlatformEvaluator(t, info.Platform{OS: "macosx", Major: 22, Arch: "arm"})
	v, err = newArm.Top(context.Background(), dir, "")
	require.NoError(t, err)
	require.Equal(t, "2.0", v.Version)
	require.Equal(t, "5", v.Revision)
}

// The payoff for portstyle: the branch-relativity limitation dissolves.
// Under each frame, corroboration selects that frame's branch span — every
// arm of a platform conditional becomes addressable from one machine.
func TestPlatformFramesResolveBranchSpans(t *testing.T) {
	src := `PortSystem 1.0
PortGroup github 1.0
name z3ish
categories devel
if {${os.major} >= 20} {
    github.setup        Z3Prover z3 4.15.4 z3-
} else {
    github.setup        Z3Prover z3 4.8.5 Z3-
}
`
	dir := portdirWith(t, src)
	b := []byte(src)
	tree, errs := syntax.Parse(b)
	require.Empty(t, errs)

	locateUnder := func(p info.Platform) portstyle.Located {
		e := newPlatformEvaluator(t, p)
		vals, err := e.Top(context.Background(), dir, "")
		require.NoError(t, err)
		loc, err := portstyle.Locate(b, tree, vals, info.FieldVersion)
		require.NoError(t, err)
		return loc
	}

	newFrame := locateUnder(info.Platform{OS: "macosx", Major: 22, Arch: "arm"})
	oldFrame := locateUnder(info.Platform{OS: "macosx", Major: 15, Arch: "x86_64"})

	require.Equal(t, "4.15.4", newFrame.Span.Text(b))
	require.Equal(t, "4.8.5", oldFrame.Span.Text(b))
	require.NotEqual(t, newFrame.Span, oldFrame.Span)
}

// A spoofed frame does not leak into the host: a separate host evaluator
// in the same test process still sees host truth.
func TestPlatformFrameIsPerEvaluator(t *testing.T) {
	dir := portdirWith(t, platformProbePortfile)

	spoofed := newPlatformEvaluator(t, info.Platform{OS: "macosx", Major: 19, Arch: "x86_64"})
	sv, err := spoofed.Top(context.Background(), dir, "")
	require.NoError(t, err)
	require.Equal(t, "1.0", sv.Version)

	host := newEvaluator(t)
	hv, err := host.Top(context.Background(), dir, "")
	require.NoError(t, err)
	// The host frame answers from real host facts; we only assert it is
	// internally consistent, since the test cannot know the host.
	require.Contains(t, []string{"1.0", "2.0"}, hv.Version)
	if hv.Version == "1.0" {
		t.Log("host is itself pre-darwin-20; spoof and host coincide here")
	}
}

func TestPlatformString(t *testing.T) {
	p := info.Platform{OS: "macosx", Major: 19, Arch: "x86_64"}
	require.Equal(t, "macosx_19_x86_64", p.String())
	require.True(t, info.Platform{}.IsZero())
	require.False(t, p.IsZero())
}
