package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
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
			got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
			require.NoError(t, err)
			require.Equal(t, operation != "captured-open" && operation != "captured-source", got.Ports["host"].ModeledHostAccess, "%v", got.Ports["host"].Problems)
		})
	}
}

// A port that reads the compiler Base picks for it, as gpsd reads
// configure.cxx with use_xcode yes, reads what Base asks the toolchain:
// which compilers exist. In a modelled context the facts table answers
// (docs/oracle.md, phase 5), so the port reads no host state and the
// compiler is the modelled release's. A release the table has no row for,
// Darwin 9, which no buildbot builds, is answered by the host as before,
// and the host read is seen.
func TestCompilerDependentFetchInputComesFromTheTable(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/host/Portfile", "PortSystem 1.0\nname host\nversion 1\nuse_xcode yes\ndistfiles host-[file tail ${configure.cxx}].tar.gz\n")
	bound, err := tree.Select(model.Target{Name: "host", Portfile: "devel/host/Portfile"})
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.False(t, got.Ports["host"].ModeledHostAccess, "%v", got.Ports["host"].Problems)
	require.Equal(t, "host-clang++.tar.gz", got.Snapshot.Ports["host"].Options["distfiles"])
	if e.Adapter == PreviewAdapter {
		// Its SDK and tool lookups are the worker's on master too
		// (toolchain.tcl's install_lookups), but get_clang_compilers, which
		// reads the tree's compiler list, still runs in the parent there.
		t.Skip("known gap on Base master: the compiler list is read in the parent interpreter, unobserved (Base design, step 5)")
	}
	got, err = e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "9", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.True(t, got.Ports["host"].ModeledHostAccess, "%v", got.Ports["host"].Problems)
}

// Eleven ports the 2026-09-24 baseline found reading the host through
// Base's compiler queries, in a modelled Darwin 22 x86_64 context, now
// read the facts table instead. DOCKHAND_TEST_PORTS_TREE names a
// macports-ports checkout.
func TestPortsThatAskBaseForCompilersReadTheTable(t *testing.T) {
	t.Parallel()
	root := os.Getenv("DOCKHAND_TEST_PORTS_TREE")
	if root == "" {
		t.Skip("set DOCKHAND_TEST_PORTS_TREE to a macports-ports checkout")
	}
	e := liveEvaluator(t)
	tree, err := macports.NewTree(model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}, root, model.Platform{})
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
			got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "22", Architecture: "x86_64"}, Declarations: true})
			require.NoError(t, err)
			require.False(t, got.Ports[port.name].ModeledHostAccess, "%v", got.Ports[port.name].Problems)
		})
	}
}

// Phase 1's dispatcher passes every call through and counts it by where
// its answer came from, judging nothing (docs/oracle.md): a read inside
// the captured tree, a host read, pure path arithmetic, and a process.
func TestTheDispatcherCountsEveryCallByItsSource(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/ledger/files/patch", "patch\n")
	putFile(t, tree.Root(), "devel/ledger/Portfile", "PortSystem 1.0\nname ledger\nversion 1\n"+
		"set here [file exists [file join ${filespath} patch]]\nset there [file exists /usr/bin/true]\nset who [exec /usr/bin/true]\n")
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "ledger"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	var reported []LedgerReport
	e.Ledger = func(report LedgerReport) { reported = append(reported, report) }
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Declarations: true})
	require.NoError(t, err)
	require.Len(t, reported, 1, "a survey hears each observed port's ledger")
	require.Equal(t, "ledger", reported[0].Port)
	require.Equal(t, got.Ports["ledger"].Ledger, reported[0].Entries)
	counted := map[string]int{}
	for _, entry := range got.Ports["ledger"].Ledger {
		counted[strings.TrimSpace(entry.Command+" "+entry.Subcommand)+" "+entry.Source] += entry.Count
	}
	require.GreaterOrEqual(t, counted["file exists tree"], 1, "%v", counted)
	require.GreaterOrEqual(t, counted["file exists host"], 1, "%v", counted)
	require.GreaterOrEqual(t, counted["file join pure"], 1, "%v", counted)
	require.GreaterOrEqual(t, counted["exec process"], 1, "%v", counted)
	require.GreaterOrEqual(t, counted["source tree"], 1, "the Portfile itself: %v", counted)
	require.GreaterOrEqual(t, counted["source base"], 1, "Base's own port1.0 is Base's, not the host's: %v", counted)
	require.True(t, got.Ports["ledger"].HostAccess, "the host read is still an event")
}

// Base's own reading of when a port last changed, source_date_epoch's
// default, first asks git whether the port's directory is in a work tree,
// which in the captured tree, a snapshot with no .git, is answered as git
// answers there, without a process, so base takes the files' modification
// times, as in the Mac's own context; and formatting the time reads Tcl's
// clock in UTC, not the Mac's zone. syft reads it in its build arguments,
// uv and cloudflared format it, and each update refused as reading the
// host (Link Dupont's report on macports-dev). The same question of a
// directory outside the tree is still the host's.
func TestSourceDateEpochIsReadFromTheCapturedTree(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/host/Portfile", "PortSystem 1.0\nname host\nversion 1\n"+
		"set built [clock format ${source_date_epoch} -format %Y-%m-%dT%H:%M:%SZ]\ndistfiles host-${source_date_epoch}-${built}.tar.gz\n")
	when := time.Unix(1700000000, 0)
	require.NoError(t, os.Chtimes(filepath.Join(tree.Root(), "devel/host/Portfile"), when, when))
	bound, err := tree.Select(model.Target{Name: "host", Portfile: "devel/host/Portfile"})
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.False(t, got.Ports["host"].ModeledHostAccess, "%v", got.Ports["host"].Problems)
	require.Equal(t, "host-1700000000-2023-11-14T22:13:20Z.tar.gz", got.Snapshot.Ports["host"].Options["distfiles"], "the Portfile's time, in UTC")

	// The same question of a directory outside the captured tree.
	putFile(t, tree.Root(), "devel/host/Portfile", "PortSystem 1.0\nname host\nversion 1\ncatch {exec git -c safe.directory=* -C "+t.TempDir()+" rev-parse --is-inside-work-tree}\n")
	got, err = e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.True(t, got.Ports["host"].ModeledHostAccess, "a directory outside the captured tree is the host's")
}
