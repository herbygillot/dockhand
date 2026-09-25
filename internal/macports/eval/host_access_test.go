package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestModeledObservationTracksHostFilesAtAccessTime(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"open", "relative-stat", "source", "captured-open", "captured-source", "symlink"} {
		t.Run(operation, func(t *testing.T) {
			e := liveEvaluator(t)
			tree := fixtureTree(t)
			external := filepath.Join(t.TempDir(), "host-data")
			require.NoError(t, os.WriteFile(external, []byte("set host_version 1.2.3"), 0600))
			captured := filepath.Join(tree.Root(), "devel/host/files/data")
			putFile(t, tree.Root(), "devel/host/files/data", "set host_version 1.2.3")
			link := filepath.Join(tree.Root(), "devel/host/files/link")
			require.NoError(t, os.Symlink(external, link))
			paths := map[string]string{"open": external, "captured-open": captured, "symlink": link}
			body := fmt.Sprintf("set handle [open {%s} r]\nset value [read $handle]\nclose $handle\n", paths[operation])
			switch operation {
			case "relative-stat":
				body = "set exists [file exists ../../../../../../tmp/dockhand-host-state]\n"
			case "source":
				body = fmt.Sprintf("source {%s}\n", external)
			case "captured-source":
				body = fmt.Sprintf("source {%s}\n", captured)
			}
			putFile(t, tree.Root(), "devel/host/Portfile", "PortSystem 1.0\nname host\nversion 1\n"+body)
			targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "host"})
			require.NoError(t, err)
			bound, err := tree.Select(targets[0])
			require.NoError(t, err)
			got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: record.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
			require.NoError(t, err)
			require.Equal(t, operation != "captured-open" && operation != "captured-source", got.Ports["host"].ModeledHostAccess, "%v", got.Ports["host"].Problems)
		})
	}
}

// A port that reads the compiler Base picks for it, as gpsd reads
// configure.cxx with use_xcode yes, reads the host: Base asks the host's
// toolchain which compilers exist. 2.12 asks in the port's worker, where
// the observation sees it; Base master asks in its parent interpreter
// (portlib.tcl), where it does not, so eleven real ports looked
// host-independent on master. The preview adapter must close that gap
// before master prepares (Base design, step 5).
func TestCompilerDependentFetchInputReadsTheHost(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	if e.Adapter == PreviewAdapter {
		t.Skip("known gap on Base master: compiler and SDK queries run in the parent interpreter, unobserved (Base design, step 5)")
	}
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/host/Portfile", "PortSystem 1.0\nname host\nversion 1\nuse_xcode yes\ndistfiles host-[file tail ${configure.cxx}].tar.gz\n")
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "host"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: record.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.True(t, got.Ports["host"].ModeledHostAccess, "%v", got.Ports["host"].Problems)
}

// The eleven ports the 2026-09-24 baseline found reading the host on 2.12.6
// and not, as observed, on Base master, in a modeled Darwin 22 x86_64
// context. DOCKHAND_TEST_PORTS_TREE names a macports-ports checkout.
func TestPortsThatReadTheHostThroughBaseCompilerQueries(t *testing.T) {
	t.Parallel()
	root := os.Getenv("DOCKHAND_TEST_PORTS_TREE")
	if root == "" {
		t.Skip("set DOCKHAND_TEST_PORTS_TREE to a macports-ports checkout")
	}
	e := liveEvaluator(t)
	if e.Adapter == PreviewAdapter {
		t.Skip("known gap on Base master: compiler and SDK queries run in the parent interpreter, unobserved (Base design, step 5)")
	}
	tree, err := macports.NewTree(record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, root, record.Platform{})
	require.NoError(t, err)
	for _, port := range []struct{ directory, name string }{
		{"aqua/qt4-mac", "qt4-mac"}, {"aqua/qt64", "qt64-qtwebengine"}, {"aqua/qt64", "qt64-qtwebengine-docs"},
		{"editors/poedit", "poedit"}, {"games/godot", "godot"}, {"java/openjdk8", "openjdk8"},
		{"lang/gcc48", "gcc48"}, {"lang/gcc49", "gcc49"}, {"lang/gcc8", "gcc8"}, {"lang/gcc8", "libgcc8"}, {"net/gpsd", "gpsd"},
	} {
		t.Run(port.name, func(t *testing.T) {
			selection := macports.Selection{Selector: port.directory}
			if filepath.Base(port.directory) != port.name {
				selection.Subport = port.name
			}
			targets, err := e.Resolve(t.Context(), tree, selection)
			require.NoError(t, err)
			bound, err := tree.Select(targets[0])
			require.NoError(t, err)
			got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: record.Platform{OS: "darwin", Version: "22", Architecture: "x86_64"}, Declarations: true})
			require.NoError(t, err)
			require.True(t, got.Ports[port.name].ModeledHostAccess, "%v", got.Ports[port.name].Problems)
		})
	}
}
