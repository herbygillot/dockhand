package macports

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func liveEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	if executable := os.Getenv("DOCKHAND_TEST_MACPORTS_TCLSH"); executable != "" {
		return &Evaluator{Executable: executable}
	}
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts port-tclsh is required for integration tests")
	}
	return &Evaluator{Executable: executable}
}

func putFile(t *testing.T, root, name, content string) {
	t.Helper()
	file := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
	require.NoError(t, os.WriteFile(file, []byte(content), 0600))
}

func fixtureTree(t *testing.T) Tree {
	t.Helper()
	root := t.TempDir()
	putFile(t, root, "_resources/port1.0/group/dockhand-fixture-1.0.tcl", "set snapshot_major 7\n")
	putFile(t, root, "devel/fixture/files/metadata.tcl", "set snapshot_minor 5\n")
	putFile(t, root, "devel/fixture/Portfile", `PortSystem 1.0
PortGroup dockhand-fixture 1.0
name fixture
categories devel
license MIT
description {A fixture with spaces and [literal text]}
long_description {Values {with braces} and \ paths}
homepage https://example.invalid
source [file join ${filespath} metadata.tcl]
version [format "%s.%s" $snapshot_major $snapshot_minor]
revision [expr {1 + 1}]
depends_lib path:lib/pkgconfig/zlib.pc:zlib
variant debug description {Enable debug} { version ${version}.debug }
subport fixture-child {
    version [expr {10 + 1}].2
    revision 7
}
`)
	tree, err := NewTree(record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, root, record.Platform{})
	require.NoError(t, err)
	return tree
}

func TestEvaluateSnapshotResourcesVariantsAndSubports(t *testing.T) {
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	variants := map[string]bool{"debug": true}
	targets, err := evaluator.Resolve(t.Context(), tree, Selection{Selector: "fixture", Variants: variants})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, "devel/fixture/Portfile", targets[0].Portfile)
	source, err := tree.Select(targets[0])
	require.NoError(t, err)
	variants["debug"] = false
	snapshot, err := evaluator.Evaluate(t.Context(), source)
	require.NoError(t, err)
	require.Len(t, snapshot.Ports, 2)
	require.Equal(t, "7.5.debug", snapshot.Ports["fixture"].Version)
	require.Equal(t, 2, snapshot.Ports["fixture"].Revision)
	require.Equal(t, "11.2.debug", snapshot.Ports["fixture-child"].Version)
	require.Equal(t, "{A fixture with spaces and [literal text]}", snapshot.Ports["fixture"].Options["description"])
	require.Contains(t, snapshot.Ports["fixture"].Dependencies, Dependency{Port: "zlib", Phase: "lib", Spec: "path:lib/pkgconfig/zlib.pc:zlib"})
	require.Equal(t, tree.Source(), snapshot.Source)
	require.NotEmpty(t, snapshot.Platform.Version)
	targets, err = evaluator.Resolve(t.Context(), tree, Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-child", Variants: map[string]bool{"debug": false}})
	require.NoError(t, err)
	require.Equal(t, "fixture-child", targets[0].Name)
	source, err = tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err = evaluator.Evaluate(t.Context(), source)
	require.NoError(t, err)
	require.Len(t, snapshot.Ports, 1)
	require.Equal(t, "11.2", snapshot.Ports["fixture-child"].Version)
}

func TestEvaluateTerraformStyleCalculatedVersion(t *testing.T) {
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.root, "sysutils/terraform/Portfile", `PortSystem 1.0
name terraform
version 1.4.0
categories sysutils
proc releaseSeries {} { global subport; return [lindex [split $subport -] 1] }
subport terraform-1.4 {
    set patch [expr {2 + 1}]
    version [join [list [releaseSeries] $patch] .]
    use_xcode yes
}
`)
	targets, err := evaluator.Resolve(t.Context(), tree, Selection{Selector: "sysutils/terraform", Subport: "terraform-1.4"})
	require.NoError(t, err)
	source, err := tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := evaluator.Evaluate(t.Context(), source)
	require.NoError(t, err)
	require.Equal(t, "1.4.3", snapshot.Ports["terraform-1.4"].Version)
	requiresXcode, err := snapshot.RequiresXcode()
	require.NoError(t, err)
	require.True(t, requiresXcode)
	require.Contains(t, snapshot.Ports["terraform-1.4"].OptionErrors, "livecheck.url")
}

func TestEvaluationDoesNotFallbackToInstalledPortGroups(t *testing.T) {
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.root, "devel/missing/Portfile", "PortSystem 1.0\nPortGroup github 1.0\nname missing\nversion 1.0\ncategories devel\n")
	_, err := evaluator.Resolve(t.Context(), tree, Selection{Selector: "missing"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "PortGroup not found")
	require.NoError(t, os.Remove(filepath.Join(tree.root, "_resources/port1.0/group/dockhand-fixture-1.0.tcl")))
	_, err = evaluator.Resolve(t.Context(), tree, Selection{Selector: "fixture"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "PortGroup not found")
}

func TestSnapshotFailsIfAnySubportFails(t *testing.T) {
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.root, "devel/broken/Portfile", "PortSystem 1.0\nname broken\nversion 1\nsubport bad { error {deliberate failure} }\n")
	targets, err := evaluator.Resolve(t.Context(), tree, Selection{Selector: "broken"})
	require.NoError(t, err)
	source, err := tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := evaluator.Evaluate(t.Context(), source)
	require.ErrorContains(t, err, "deliberate failure")
	require.Empty(t, snapshot.Ports)
}

func TestResolutionRefusesAmbiguityTraversalAndMissingSubports(t *testing.T) {
	tree := fixtureTree(t)
	evaluator := &Evaluator{Executable: "missing-shell"}
	putFile(t, tree.root, "other/fixture/Portfile", "PortSystem 1.0\nname fixture\nversion 1\n")
	for _, selector := range []string{"fixture", "../fixture", "/devel/fixture", "missing", "all", "devel/fixture/extra/Portfile"} {
		_, err := evaluator.Resolve(t.Context(), tree, Selection{Selector: selector})
		require.ErrorIs(t, err, ErrTarget)
	}
	evaluator = liveEvaluator(t)
	_, err := evaluator.Resolve(t.Context(), tree, Selection{Selector: "devel/fixture", Subport: "not-a-subport"})
	require.Error(t, err)
}

func TestEvaluationRejectsPlatformMismatchAndCancellation(t *testing.T) {
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	tree.platform = record.Platform{OS: "darwin", Version: "1", Architecture: "arm64"}
	_, err := evaluator.Resolve(t.Context(), tree, Selection{Selector: "fixture"})
	require.ErrorIs(t, err, ErrPlatform)
	tree.platform = record.Platform{}
	putFile(t, tree.root, "devel/hang/Portfile", "PortSystem 1.0\nname hang\nversion 1\nafter 30000\n")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	started := time.Now()
	_, err = evaluator.Resolve(ctx, tree, Selection{Selector: "hang"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 8*time.Second)
}

func TestDecodeMetadataPreservesTclValuesAndDependencySyntax(t *testing.T) {
	info, subs, err := decodeMetadata(`name {demo} version 1.2 revision 0 epoch 0 description {a {b} [c] $d} depends_run {port:foo bin:bar:provider} subports {child}`)
	require.NoError(t, err)
	require.Equal(t, []string{"child"}, subs)
	require.Equal(t, "a {b} [c] $d", info.Options["description"])
	require.Equal(t, []Dependency{{Port: "foo", Phase: "run", Spec: "port:foo"}, {Port: "provider", Phase: "run", Spec: "bin:bar:provider"}}, info.Dependencies)
	for _, reply := range []string{"name {", "name demo version 1 revision x epoch 0", "name demo version 1 revision 0 epoch 0 depends_run {foo}", "name demo version 1 revision -1 epoch 0"} {
		_, _, err := decodeMetadata(reply)
		require.Error(t, err)
	}
}

func TestSnapshotRequiresXcode(t *testing.T) {
	makeSnapshot := func(value string) Snapshot {
		return Snapshot{Target: record.Target{Name: "fixture"}, Ports: map[string]PortInfo{"fixture": {Name: "fixture", Options: map[string]string{"use_xcode": value}}}}
	}
	for _, value := range []string{"yes", "true", "1", "on"} {
		required, err := makeSnapshot(value).RequiresXcode()
		require.NoError(t, err)
		require.True(t, required)
	}
	for _, value := range []string{"", "no", "false", "0", "off"} {
		required, err := makeSnapshot(value).RequiresXcode()
		require.NoError(t, err)
		require.False(t, required)
	}
	_, err := makeSnapshot("perhaps").RequiresXcode()
	require.ErrorContains(t, err, "invalid use_xcode")
	snapshot := makeSnapshot("no")
	snapshot.Ports["fixture"] = PortInfo{Name: "fixture", Options: map[string]string{"use_xcode": "no"}, OptionErrors: map[string]string{"use_xcode": "failed"}}
	_, err = snapshot.RequiresXcode()
	require.ErrorContains(t, err, "failed")
}

func TestStartupErrorsAreNotSuccessfulHandshakes(t *testing.T) {
	executable, err := exec.LookPath("tclsh")
	require.NoError(t, err)
	evaluator := &Evaluator{Executable: executable}
	_, err = evaluator.NativePlatform(t.Context())
	require.True(t, errors.Is(err, ErrStartup))
}

func TestEvaluationExposesComputedUpstreamTagMetadata(t *testing.T) {
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.root, "devel/tagged/Portfile", `PortSystem 1.0
name tagged
version 1.8.1
options github.version github.tag_prefix github.tag_suffix git.branch
github.version ${version}
github.tag_prefix release/
github.tag_suffix -stable
git.branch ${github.tag_prefix}${github.version}${github.tag_suffix}
`)
	targets, err := evaluator.Resolve(t.Context(), tree, Selection{Selector: "tagged"})
	require.NoError(t, err)
	source, err := tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := evaluator.Evaluate(t.Context(), source)
	require.NoError(t, err)
	info := snapshot.Ports["tagged"]
	require.Equal(t, "release/1.8.1-stable", info.Options["git.branch"])
	require.Equal(t, "release/", info.Options["github.tag_prefix"])
	require.Equal(t, "-stable", info.Options["github.tag_suffix"])
	require.Equal(t, "1.8.1", info.Options["github.version"])
}
