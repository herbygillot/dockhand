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
	bound, err := tree.Select(record.Target{Name: "host", Portfile: "devel/host/Portfile"})
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: record.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.False(t, got.Ports["host"].ModeledHostAccess, "%v", got.Ports["host"].Problems)
	require.Equal(t, "host-clang++.tar.gz", got.Snapshot.Ports["host"].Options["distfiles"])
	if e.Adapter == PreviewAdapter {
		t.Skip("known gap on Base master: compiler and SDK queries run in the parent interpreter, unobserved (Base design, step 5)")
	}
	got, err = e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: record.Platform{OS: "darwin", Version: "9", Architecture: "x86_64"}, Declarations: true})
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
