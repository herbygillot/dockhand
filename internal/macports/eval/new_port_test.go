package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portcreate"
	"github.com/herbygillot/dockhand/internal/model"
)

// Each kind of new port create writes, as it fills it, evaluates with
// MacPorts' real PortGroups (the corpus's pinned _resources) for a release
// that isn't this Mac's, so the gate on either Mac evaluates a new port in
// a modelled context: a new Go port's go.vendors had the golang PortGroup
// read extract.cmd, whose gzip a modelled release didn't have, and every
// new Go port ended unanswered there; no row or test evaluated one so
// (the v0.9.0-rc2 full stage's B8).
func TestEachKindOfNewPortEvaluatesInAModelledRelease(t *testing.T) {
	t.Parallel()
	resources, err := filepath.Abs("../../../testdata/corpus/tree/_resources")
	require.NoError(t, err)
	platform := monterey
	var uname unix.Utsname
	require.NoError(t, unix.Uname(&uname))
	host, _, _ := strings.Cut(unix.ByteSliceToString(uname.Release[:]), ".")
	if host == platform.Version {
		platform = model.Platform{OS: "darwin", Version: "23", Architecture: "arm64"}
	}
	base := portcreate.Spec{Category: "devel", Owner: "example", Project: "tool", Version: "1.2.3", TagPrefix: "v",
		Description: "A tool for testing", License: "MIT", Binaries: []string{"tool"}}
	kinds := []struct {
		system string
		spec   func(portcreate.Spec) portcreate.Spec
		fill   func(string) string
	}{
		{"go", func(s portcreate.Spec) portcreate.Spec { return s }, func(text string) string {
			// go.vendors as create fills it from go2port: a non-empty
			// list is what has the PortGroup rewrite extract.cmd.
			return strings.Replace(text, "\ngo.vendors\n", "\ngo.vendors          golang.org/x/sys \\\n"+
				"                        lock    v0.1.0 \\\n"+
				"                        rmd160  0000000000000000000000000000000000000000 \\\n"+
				"                        sha256  0000000000000000000000000000000000000000000000000000000000000000 \\\n"+
				"                        size    1\n", 1)
		}},
		{"cargo", func(s portcreate.Spec) portcreate.Spec {
			s.Crates = []portcreate.Crate{{Name: "anyhow", Version: "1.0.89", Checksum: "86fdf8605db99b54d3cd748a44c6d04df638eb5dafb219b135d0149bd0db01f6"}}
			return s
		}, nil},
		{"python", func(s portcreate.Spec) portcreate.Spec { s.PythonVersion = "313"; return s }, nil},
		{"", func(s portcreate.Spec) portcreate.Spec { return s }, nil},
	}
	for _, kind := range kinds {
		name := "new-" + kind.system
		if kind.system == "" {
			name = "new-plain"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := kind.spec(base)
			spec.Name = name
			spec.Build = portcreate.Build{System: kind.system}
			text := string(portcreate.Write(spec))
			if kind.fill != nil {
				filled := kind.fill(text)
				require.NotEqual(t, text, filled, "the block create fills is where it writes it")
				text = filled
			}
			root := t.TempDir()
			require.NoError(t, os.CopyFS(filepath.Join(root, "_resources"), os.DirFS(resources)))
			putFile(t, root, "devel/"+name+"/Portfile", text)
			tree, err := macports.NewTree(model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}, root, model.Platform{})
			require.NoError(t, err)
			bound, err := tree.Select(model.Target{Name: name, Portfile: "devel/" + name + "/Portfile"})
			require.NoError(t, err)
			observed, err := liveEvaluator(t).Observe(t.Context(), bound, macports.ObservationRequest{Declarations: true, Platform: platform})
			require.NoError(t, err, "a new %s port evaluates for Darwin %s:\n%s", name, platform.Version, text)
			require.Contains(t, observed.Snapshot.Ports, name)
		})
	}
}
