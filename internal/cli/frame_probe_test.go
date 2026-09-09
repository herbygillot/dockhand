package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// TestFrameProbe asks each simulated platform frame what it actually
// saw. It ships nothing.
//
// dockhand simulates a platform with macports::override_vars, which
// base accepts for any variable in its own namespace. The frame it
// writes names six: os_platform, os_major, os_version, os_arch,
// os_subplatform and cxx_stdlib. base exports FIFTEEN platform
// variables to the Portfile interpreter, and a port may branch on any of
// them to choose its distfiles.
//
// The question this answers is not what the frame SETS but what a
// Portfile SEES — because base derives some of these from others at
// mportinit, before override_vars has run, and a derived value does not
// follow the variable it was derived from.
//
//	DOCKHAND_FRAME_PROBE=1 go test ./internal/cli/ -run TestFrameProbe -v
func TestFrameProbe(t *testing.T) {
	if os.Getenv("DOCKHAND_FRAME_PROBE") == "" {
		t.Skip("set DOCKHAND_FRAME_PROBE=1 to run")
	}
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, macports.PortGroupDir), 0o755))
	pd := filepath.Join(root, "devel", "probe")
	require.NoError(t, os.MkdirAll(pd, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pd, "Portfile"),
		[]byte("PortSystem 1.0\nname probe\nversion 1.0\ncategories devel\n"+
			"maintainers nomaintainer\nlicense MIT\ndescription p\nlong_description p\n"+
			"homepage https://example.invalid/\nplatforms darwin\n"), 0o644))

	ctx := context.Background()
	pfx, err := prefix.Find(testFinder())
	require.NoError(t, err, "this probe needs a real MacPorts installation")

	// The variables a Portfile can branch on to pick a distfile, in the
	// spellings a Portfile uses.
	asked := []string{
		"os.platform", "os.major", "os.minor", "os.version", "os.arch",
		"os.subplatform", "os.endian",
		"build_arch", "configure.build_arch", "universal_archs",
		"configure.universal_archs", "supported_archs",
		"configure.cxx_stdlib", "macosx_deployment_target",
		"macos_version", "macosx_sdk_version",
	}

	frames := []struct {
		what string
		p    info.Platform
	}{
		{"tahoe/arm", info.Platform{OS: "macosx", Major: 25, Arch: "arm"}},
		{"tahoe/i386", info.Platform{OS: "macosx", Major: 25, Arch: "i386"}},
		{"sierra/arm", info.Platform{OS: "macosx", Major: 16, Arch: "arm"}},
	}

	got := map[string]map[string]string{}
	for _, f := range frames {
		p, err := pool.New(ctx, pfx, 1, eval.WithPlatform(f.p))
		require.NoError(t, err, f.what)
		h := port.New(tree.Target{Portdir: pd}, p.Evaluators()[0])
		opts, oerr := h.Options(ctx, asked...)
		require.NoError(t, oerr, f.what)
		got[f.what] = opts
		p.Close()
	}

	fmt.Fprintf(os.Stderr, "\n%-28s %-18s %-18s %-18s\n", "variable", frames[0].what, frames[1].what, frames[2].what)
	for _, a := range asked {
		v0, v1, v2 := got[frames[0].what][a], got[frames[1].what][a], got[frames[2].what][a]
		mark := "  "
		if v0 == v1 {
			mark = "!!" // the arch frame changed nothing
		}
		fmt.Fprintf(os.Stderr, "%s %-26s %-18s %-18s %-18s\n", mark, a, trunc(v0), trunc(v1), trunc(v2))
	}
	fmt.Fprintln(os.Stderr, "\n!! = identical across the two ARCH frames: the override did not reach it")
}

func trunc(s string) string {
	if len(s) > 17 {
		return s[:16] + "…"
	}
	if s == "" {
		return "(empty)"
	}
	return s
}
