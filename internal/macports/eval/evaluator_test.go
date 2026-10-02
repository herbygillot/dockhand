package eval

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func liveEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	// DOCKHAND_TEST_BASE_ADAPTER=preview runs the suite against a
	// development build of Base, which the evaluator otherwise refuses.
	return &Evaluator{Executable: testsupport.MacPortsTclsh(t), Adapter: testsupport.BaseAdapter()}
}

func putFile(t *testing.T, root, name, content string) {
	t.Helper()
	file := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
	require.NoError(t, os.WriteFile(file, []byte(content), 0600))
}

func fixtureTree(t *testing.T) macports.Tree {
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
	tree, err := macports.NewTree(model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}, root, model.Platform{})
	require.NoError(t, err)
	return tree
}

func TestEvaluateSnapshotResourcesVariantsAndSubports(t *testing.T) {
	t.Parallel()
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	variants := map[string]bool{"debug": true}
	targets, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture", Variants: variants})
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
	require.Contains(t, snapshot.Ports["fixture"].Dependencies, macports.Dependency{Port: "zlib", Phase: "lib", Spec: "path:lib/pkgconfig/zlib.pc:zlib"})
	require.Equal(t, tree.Source(), snapshot.Source)
	require.NotEmpty(t, snapshot.Platform.Version)
	targets, err = evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "devel/fixture/Portfile", Subport: "fixture-child", Variants: map[string]bool{"debug": false}})
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
	t.Parallel()
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "sysutils/terraform/Portfile", `PortSystem 1.0
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
	targets, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "sysutils/terraform", Subport: "terraform-1.4"})
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
	t.Parallel()
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/missing/Portfile", "PortSystem 1.0\nPortGroup github 1.0\nname missing\nversion 1.0\ncategories devel\n")
	_, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "missing"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "PortGroup not found")
	require.NoError(t, os.Remove(filepath.Join(tree.Root(), "_resources/port1.0/group/dockhand-fixture-1.0.tcl")))
	_, err = evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "PortGroup not found")
}

func TestSnapshotFailsIfAnySubportFails(t *testing.T) {
	t.Parallel()
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/broken/Portfile", "PortSystem 1.0\nname broken\nversion 1\nsubport bad { error {deliberate failure} }\n")
	targets, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "broken"})
	require.NoError(t, err)
	source, err := tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := evaluator.Evaluate(t.Context(), source)
	require.ErrorContains(t, err, "deliberate failure")
	require.Empty(t, snapshot.Ports)
}

func TestResolutionRefusesAmbiguityTraversalAndMissingSubports(t *testing.T) {
	t.Parallel()
	tree := fixtureTree(t)
	evaluator := &Evaluator{Executable: "missing-shell"}
	putFile(t, tree.Root(), "other/fixture/Portfile", "PortSystem 1.0\nname fixture\nversion 1\n")
	for _, selector := range []string{"fixture", "../fixture", "/devel/fixture", "missing", "all", "devel/fixture/extra/Portfile"} {
		_, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: selector})
		require.ErrorIs(t, err, macports.ErrTarget)
	}
	for _, selector := range []string{"devel/fixture/files", "_resources/fixture/Portfile"} {
		_, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: selector})
		require.ErrorIs(t, err, macports.ErrTarget)
		require.ErrorContains(t, err, "expected category/port/Portfile", selector)
	}
	evaluator = liveEvaluator(t)
	_, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "devel/fixture", Subport: "not-a-subport"})
	require.Error(t, err)
}

func TestEvaluationRejectsPlatformMismatchAndCancellation(t *testing.T) {
	t.Parallel()
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	tree, err := macports.NewTree(tree.Source(), tree.Root(), model.Platform{OS: "darwin", Version: "1", Architecture: "arm64"})
	require.NoError(t, err)
	_, err = evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture"})
	require.ErrorIs(t, err, macports.ErrPlatform)
	tree, err = macports.NewTree(tree.Source(), tree.Root(), model.Platform{})
	require.NoError(t, err)
	putFile(t, tree.Root(), "devel/hang/Portfile", "PortSystem 1.0\nname hang\nversion 1\nafter 30000\n")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	started := time.Now()
	_, err = evaluator.Resolve(ctx, tree, macports.Selection{Selector: "hang"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 8*time.Second)
}

func TestDecodeMetadataPreservesTclValuesAndDependencySyntax(t *testing.T) {
	t.Parallel()
	info, subs, err := decodeMetadata(`name {demo} version 1.2 revision 0 epoch 0 description {a {b} [c] $d} depends_run {port:foo bin:bar:provider} depends_build {port:bin/cmake:cmake} subports {child}`)
	require.NoError(t, err)
	require.Equal(t, []string{"child"}, subs)
	require.Equal(t, "a {b} [c] $d", info.Options["description"])
	require.Equal(t, []macports.Dependency{{Port: "cmake", Phase: "build", Spec: "port:bin/cmake:cmake"},
		{Port: "foo", Phase: "run", Spec: "port:foo"}, {Port: "provider", Phase: "run", Spec: "bin:bar:provider"}}, info.Dependencies, "the port is the last field, as Base reads it")
	for _, reply := range []string{"name {", "name demo version 1 revision x epoch 0", "name demo version 1 revision 0 epoch 0 depends_run {foo}", "name demo version 1 revision -1 epoch 0"} {
		_, _, err := decodeMetadata(reply)
		require.Error(t, err)
	}
}

func TestStartupErrorsAreNotSuccessfulHandshakes(t *testing.T) {
	t.Parallel()
	executable, err := exec.LookPath("tclsh")
	if err != nil {
		t.Skip("tclsh is required")
	}
	evaluator := &Evaluator{Executable: executable}
	_, err = evaluator.NativePlatform(t.Context())
	require.True(t, errors.Is(err, macports.ErrStartup))
}

func TestEvaluationExposesComputedUpstreamTagMetadata(t *testing.T) {
	t.Parallel()
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/tagged/Portfile", `PortSystem 1.0
name tagged
version 1.8.1
options github.version github.tag_prefix github.tag_suffix git.branch
github.version ${version}
github.tag_prefix release/
github.tag_suffix -stable
git.branch ${github.tag_prefix}${github.version}${github.tag_suffix}
`)
	targets, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "tagged"})
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

func TestSelectedEvaluationLeavesSiblingFailuresToFullValidation(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/selected/Portfile", `PortSystem 1.0
name selected
version 1
subport selected-broken { error "sibling requires attention" }
`)
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "selected"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := e.EvaluateSelected(t.Context(), bound)
	require.NoError(t, err)
	require.Len(t, snapshot.Ports, 1)
	observed, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Declarations: true, SelectedOnly: true})
	require.NoError(t, err)
	require.Len(t, observed.Snapshot.Ports, 1)
	_, err = e.Evaluate(t.Context(), bound)
	require.ErrorContains(t, err, "sibling requires attention")
	_, err = e.Observe(t.Context(), bound, macports.ObservationRequest{Declarations: true})
	require.ErrorContains(t, err, "sibling requires attention")
}

// A port's default livecheck, with a plain master site, resolves through
// the tree's master-sites.tcl, which reads livecheck.distname: every such
// port read as unresolved, type default with no regex, while the resolver
// didn't take Base's globals, libt3config and tilde among them (batch 30).
func TestTheDefaultLivecheckResolvesFromTheMasterSite(t *testing.T) {
	t.Parallel()
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "_resources/port1.0/livecheck/fallback.tcl", "source [getdefaultportresourcepath \"port1.0/livecheck\"]/master-sites.tcl\n")
	putFile(t, tree.Root(), "_resources/port1.0/livecheck/master-sites.tcl", `set livecheck.type "regex"
if {${livecheck.name} eq "default"} {
    set livecheck.name ${name}
}
if {${livecheck.distname} eq "default"} {
    set livecheck.distname ${livecheck.name}
}
if {!$has_homepage || ${livecheck.url} eq ${homepage}} {
    if {!$has_master_sites || [llength ${master_sites}] == 0} {
        set livecheck.type "none"
    } else {
        set livecheck.url [lindex ${master_sites} 0]
    }
}
if {${livecheck.regex} eq ""} {
    set livecheck.regex [list "[quotemeta ${livecheck.distname}]-(\\d+(?:\\.\\d+)*)"]
}
`)
	putFile(t, tree.Root(), "devel/tilde/Portfile", "PortSystem 1.0\nname tilde\nversion 1.1.3\ncategories devel\nhomepage https://os.example.org/\nmaster_sites ${homepage}/dist/\n")
	targets, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "devel/tilde"})
	require.NoError(t, err)
	source, err := tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := evaluator.Evaluate(t.Context(), source)
	require.NoError(t, err)
	port := snapshot.Ports["tilde"]
	require.Equal(t, "default", port.Options["dockhand.livecheck_declared"])
	require.Equal(t, "regex", port.Options["livecheck.type"])
	require.Equal(t, "https://os.example.org//dist/", port.Options["livecheck.url"])
	require.Contains(t, port.Options["livecheck.regex"], "tilde-")
}

// A livecheck type such as pypi is resolved through the tree's own checker
// definitions, exactly as port livecheck does, so dockhand sees the regex
// livecheck it stands for rather than a type it would have to understand.
func TestEvaluateResolvesLivecheckTypesThroughTheTreesCheckers(t *testing.T) {
	t.Parallel()
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "_resources/port1.0/livecheck/pypi.tcl", `if {${livecheck.name} eq "default"} {
    livecheck.name ${name}
}
if {!$has_homepage || ${livecheck.url} eq ${homepage}} {
    livecheck.url https://pypi.org/pypi/${livecheck.name}/json
}
if {${livecheck.regex} eq ""} {
    livecheck.regex {"version": *"([^"]+)"[,\}]}
}
set livecheck.type "regex"
`)
	putFile(t, tree.Root(), "python/py-foo/Portfile", `PortSystem 1.0
name py-foo
version 1.2
categories python
homepage https://example.invalid/foo
master_sites https://example.invalid/
livecheck.type pypi
livecheck.name foo
`)
	targets, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "python/py-foo"})
	require.NoError(t, err)
	source, err := tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := evaluator.Evaluate(t.Context(), source)
	require.NoError(t, err)
	port := snapshot.Ports["py-foo"]
	require.Equal(t, "pypi", port.Options["dockhand.livecheck_declared"])
	require.Equal(t, "regex", port.Options["livecheck.type"])
	require.Equal(t, "https://pypi.org/pypi/foo/json", port.Options["livecheck.url"])
	require.Equal(t, `{"version": *"([^"]+)"[,\}]}`, port.Options["livecheck.regex"], "option values keep their list encoding, as the raw path does")
	require.NotContains(t, port.OptionErrors, "livecheck.url")
}

// A host that is not a Mac describes the current macOS with its Command Line
// Tools: a port that compiles takes Apple's clang, even one that refuses old
// clangs, and needs no Xcode, as on a Mac with the tools installed. Another
// release is modeled when asked. A Mac describes itself, so this runs only
// where the host is not one.
func TestModeledHostDescribesAMacWithTheCommandLineTools(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	runtime, err := e.Inspect(t.Context())
	require.NoError(t, err)
	if !runtime.Modeled() {
		t.Skip("the host is a Mac and describes itself")
	}
	require.Equal(t, DefaultModel(), runtime.Platform)
	require.NotEqual(t, "darwin", runtime.Host.OS)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/compiled/Portfile", `PortSystem 1.0
name compiled
version 1.0
categories devel
license MIT
maintainers nomaintainer
description compiled
long_description compiled
homepage https://example.invalid
master_sites https://example.invalid
compiler.blacklist-append {clang < 1300}
`)
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "compiled"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := e.Evaluate(t.Context(), bound)
	require.NoError(t, err)
	require.Equal(t, DefaultModel(), snapshot.Platform)
	port := snapshot.Ports["compiled"]
	for _, dependency := range port.Dependencies {
		require.NotContains(t, dependency.Port, "clang", "Apple's clang is the compiler")
	}
	require.Equal(t, "0", port.Options["use_xcode"])
	older := &Evaluator{Executable: e.Executable, Model: model.Platform{OS: "darwin", Version: "23", Architecture: "x86_64"}}
	chosen, err := older.NativePlatform(t.Context())
	require.NoError(t, err)
	require.Equal(t, older.Model, chosen)
}

// Whether a port declares tests is MacPorts' reading of test.run, which
// Base gives no default: set, or unset, and so off.
func TestTheEvaluatorReadsWhetherAPortDeclaresTests(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/tested/Portfile", "PortSystem 1.0\nname tested\nversion 1\ntest.run yes\n")
	putFile(t, tree.Root(), "devel/untested/Portfile", "PortSystem 1.0\nname untested\nversion 1\n")
	for name, want := range map[string]bool{"tested": true, "untested": false} {
		targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: name})
		require.NoError(t, err)
		bound, err := tree.Select(targets[0])
		require.NoError(t, err)
		snapshot, err := e.Evaluate(t.Context(), bound)
		require.NoError(t, err)
		tests, err := snapshot.Ports[name].Bool("dockhand.test_run")
		require.NoError(t, err)
		require.Equal(t, want, tests, name)
	}
}

// The PortGroups a port loads are reported by name, as Base records them,
// and a port that loads none reports an empty list, not nothing.
func TestTheEvaluatorReportsThePortGroupsAPortLoads(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/plain/Portfile", "PortSystem 1.0\nname plain\nversion 1\n")
	for name, want := range map[string]string{"fixture": "dockhand-fixture", "plain": ""} {
		targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: name})
		require.NoError(t, err)
		bound, err := tree.Select(targets[0])
		require.NoError(t, err)
		snapshot, err := e.Evaluate(t.Context(), bound)
		require.NoError(t, err)
		groups, set := snapshot.Ports[name].Options["dockhand.portgroups"]
		require.True(t, set, name)
		require.Equal(t, want, groups, name)
		require.Equal(t, "./configure", snapshot.Ports[name].Options["configure.cmd"], name)
	}
}

// The Go a golang PortGroup port pins is read as MacPorts evaluates it:
// go.bin, with ${prefix} as Base sets it, beside the dependencies, as
// trivy pinned go-1.26 by both (the trivy run, #35083); a port building
// with the PortGroup's own ${prefix}/bin/go and port:go pins nothing. The
// PortGroup is a stand-in with MacPorts' own option and defaults.
func TestTheEvaluatorReadsTheGoAPortPins(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "_resources/port1.0/group/golang-1.0.tcl", "options go.bin\ndefault go.bin {${prefix}/bin/go}\ndefault depends_build port:go\n")
	putFile(t, tree.Root(), "security/trivy/Portfile", "PortSystem 1.0\nPortGroup golang 1.0\nname trivy\nversion 0.75.0\ndepends_build port:go-1.26\ngo.bin ${prefix}/bin/go-1.26\n")
	putFile(t, tree.Root(), "devel/current/Portfile", "PortSystem 1.0\nPortGroup golang 1.0\nname current\nversion 1\n")
	evaluated := func(name string) macports.PortInfo {
		t.Helper()
		targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: name})
		require.NoError(t, err)
		bound, err := tree.Select(targets[0])
		require.NoError(t, err)
		snapshot, err := e.Evaluate(t.Context(), bound)
		require.NoError(t, err)
		return snapshot.Ports[name]
	}
	trivy := evaluated("trivy")
	require.Equal(t, "go-1.26", path.Base(trivy.Options["go.bin"]))
	require.True(t, path.IsAbs(trivy.Options["go.bin"]), "${prefix} is substituted: %s", trivy.Options["go.bin"])
	pin, pinned := trivy.GoPinned()
	require.True(t, pinned)
	require.Equal(t, macports.GoPin{Series: "1.26", By: []string{"go.bin", "depends_build"}}, pin)

	current := evaluated("current")
	require.Equal(t, "go", path.Base(current.Options["go.bin"]))
	_, pinned = current.GoPinned()
	require.False(t, pinned)
}

// A python PortGroup port's Pythons are read as MacPorts evaluates them,
// its python.versions, python.version, and python.default_version, and the
// default the PortGroup would give it, from its own
// python_get_default_version; a port without the PortGroup has none. The
// PortGroup is a stand-in with MacPorts' own options, defaults, and procs,
// python_set_default_version's option_proc included: a port not named py-
// that pins the version has python.versions set to the pin, which the
// stand-in once left out, so sshuttle's pin read as the default (the
// sshuttle run with f075232d).
func TestTheEvaluatorReadsAPortsPythons(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "_resources/port1.0/group/python-1.0.tcl", `options python.versions python.version python.default_version
default python.default_version {[python_get_default_version]}
default python.version {[python_get_version]}
proc python_get_version {} {
    if {[string match py-* [option name]]} {
        return [string range [option subport] 2 [string first "-" [option subport]]-1]
    } else {
        return [option python.default_version]
    }
}
proc python_get_default_version {} {
    global python.versions
    set def_v 314
    if {[info exists python.versions] && ${def_v} ni ${python.versions}} {
        return [lindex ${python.versions} end]
    } else {
        return ${def_v}
    }
}
option_proc python.default_version python_set_default_version
proc python_set_default_version {option action args} {
    if {$action ne "set"} {
        return
    }
    if {![string match py-* [option name]]} {
        python.versions [option python.default_version]
    }
}
`)
	putFile(t, tree.Root(), "net/sshuttle/Portfile", "PortSystem 1.0\nPortGroup python 1.0\nname sshuttle\nversion 2.0.0\npython.default_version 313\n")
	putFile(t, tree.Root(), "python/py-demo/Portfile", "PortSystem 1.0\nPortGroup python 1.0\nname py-demo\nversion 1\npython.versions 312 313\n")
	putFile(t, tree.Root(), "net/current/Portfile", "PortSystem 1.0\nPortGroup python 1.0\nname current\nversion 1\n")
	putFile(t, tree.Root(), "python/py-behind/Portfile", "PortSystem 1.0\nPortGroup python 1.0\nname py-behind\nversion 1\npython.versions 312 313 314\npython.default_version 313\n")
	putFile(t, tree.Root(), "devel/plain/Portfile", "PortSystem 1.0\nname plain\nversion 1\n")
	evaluated := func(name string) macports.PortInfo {
		t.Helper()
		targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: name})
		require.NoError(t, err)
		bound, err := tree.Select(targets[0])
		require.NoError(t, err)
		snapshot, err := e.Evaluate(t.Context(), bound)
		require.NoError(t, err)
		return snapshot.Ports[name]
	}
	sshuttle := evaluated("sshuttle")
	require.Equal(t, []string{"3.13"}, sshuttle.Pythons())
	pinned, standard, ok := sshuttle.PythonPinned()
	require.True(t, ok)
	require.Equal(t, [2]string{"3.13", "3.14"}, [2]string{pinned, standard})

	demo := evaluated("py-demo")
	require.Equal(t, []string{"3.12", "3.13"}, demo.Pythons())
	pinned, standard, ok = demo.PythonPinned()
	require.True(t, ok)
	require.Equal(t, [2]string{"3.13", "3.13"}, [2]string{pinned, standard}, "the PortGroup's default is the newest the port builds for, without 3.14")

	pinned, standard, ok = evaluated("py-behind").PythonPinned()
	require.True(t, ok)
	require.Equal(t, [2]string{"3.13", "3.14"}, [2]string{pinned, standard}, "a py- port's own python.versions reach 3.14")

	pinned, standard, ok = evaluated("current").PythonPinned()
	require.True(t, ok)
	require.Equal(t, [2]string{"3.14", "3.14"}, [2]string{pinned, standard}, "a port that pins nothing has the default")

	plain := evaluated("plain")
	require.Empty(t, plain.Pythons())
	_, _, ok = plain.PythonPinned()
	require.False(t, ok)
}

// A probe of whether a port builds anything that fails is recorded as a
// failure, not taken for a port that builds, as it had been (the
// code-organization review's finding 27).
func TestAFailedMetadataProbeIsRecorded(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/unsure/Portfile", "PortSystem 1.0\nname unsure\nversion 1\ntrace add variable distfiles read {apply {args {error boom}}}\n")
	targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: "unsure"})
	require.NoError(t, err)
	bound, err := tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := e.Evaluate(t.Context(), bound)
	require.NoError(t, err)
	_, err = snapshot.Ports["unsure"].MetadataOnly()
	require.ErrorContains(t, err, "cannot tell whether the port builds anything")
}

// Whether a port is known to fail, and whether its platforms exclude the
// release, are MacPorts' own answers: beekeeper-studio declares no
// known_fail, and its platforms {darwin >= 23} make MacPorts mark it known
// to fail on macOS 12, which a plan names for its platforms (the
// beekeeper-studio run's finding 3). known_fail on is true, as Tcl reads it.
func TestTheEvaluatorSaysWhyAPortIsKnownToFail(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/newer/Portfile", "PortSystem 1.0\nname newer\nversion 1\nplatforms {darwin >= 23}\n")
	putFile(t, tree.Root(), "devel/failing/Portfile", "PortSystem 1.0\nname failing\nversion 1\nknown_fail on\n")
	observe := func(name, version string) macports.PortInfo {
		t.Helper()
		targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: name})
		require.NoError(t, err)
		bound, err := tree.Select(targets[0])
		require.NoError(t, err)
		got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: version, Architecture: "arm64"}})
		require.NoError(t, err)
		return got.Snapshot.Ports[name]
	}
	old := observe("newer", "21")
	eligibility, err := macports.BuildEligibility(old, model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"})
	require.NoError(t, err)
	require.Equal(t, "its platforms, {darwin >= 23}, exclude this release", eligibility.Reason())
	current := observe("newer", "25")
	eligibility, err = macports.BuildEligibility(current, model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"})
	require.NoError(t, err)
	require.True(t, eligibility.Eligible())
	eligibility, err = macports.BuildEligibility(observe("failing", "25"), model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"})
	require.NoError(t, err)
	require.Equal(t, macports.ExcludedKnownFail, eligibility.Excluded)
}

// A port's minimum_xcodeversions is read against the Xcode the evaluation
// models for the macOS: the one it asks of this macOS where that Xcode is
// older, and nothing where it's met or asked of another, where
// sand-runner's {24 26.0} read as a failed fetch on macOS 15 with Xcode
// 16.4 (the sand-runner port).
func TestTheEvaluatorReadsAMinimumXcode(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/swifty/Portfile", "PortSystem 1.0\nname swifty\nversion 1\noptions minimum_xcodeversions\ndefault minimum_xcodeversions {}\nminimum_xcodeversions {25 99.0 24 1.0}\n")
	putFile(t, tree.Root(), "devel/plain/Portfile", "PortSystem 1.0\nname plain\nversion 1\n")
	observe := func(name, version string) macports.PortInfo {
		t.Helper()
		targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: name})
		require.NoError(t, err)
		bound, err := tree.Select(targets[0])
		require.NoError(t, err)
		got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: version, Architecture: "arm64"}})
		require.NoError(t, err)
		return got.Snapshot.Ports[name]
	}
	for _, c := range []struct{ name, version, minimum string }{{"swifty", "25", "99.0"}, {"swifty", "24", ""}, {"swifty", "23", ""}, {"plain", "25", ""}} {
		minimum, err := observe(c.name, c.version).MinimumXcode()
		require.NoError(t, err)
		require.Equal(t, c.minimum, minimum, "%s on darwin %s", c.name, c.version)
	}
}

// The variants a port declares come from Base's own record of them, its
// universal among them:
// which are defaults, what each requires and conflicts with, and its
// description (PortInfo(variants), PortInfo(vinfo)).
func TestTheEvaluatorReportsTheVariantsAPortDeclares(t *testing.T) {
	t.Parallel()
	evaluator := liveEvaluator(t)
	root := t.TempDir()
	putFile(t, root, "devel/harbor/Portfile", `PortSystem 1.0
name harbor
version 1.0
categories devel
license MIT
description Harbor
long_description Harbor
homepage https://example.invalid
variant tests description {Build and run the tests} {}
variant docs requires tests description {Documentation} {}
variant python312 conflicts python313 description {Python 3.12} {}
variant python313 conflicts python312 description {Python 3.13} {}
default_variants +python313
`)
	tree, err := macports.NewTree(model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}, root, model.Platform{})
	require.NoError(t, err)
	targets, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "harbor"})
	require.NoError(t, err)
	source, err := tree.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := evaluator.Evaluate(t.Context(), source)
	require.NoError(t, err)
	variants, err := snapshot.Ports["harbor"].Variants()
	require.NoError(t, err)
	require.Equal(t, []macports.Variant{
		{Name: "tests", Description: "Build and run the tests"},
		{Name: "docs", Requires: []string{"tests"}, Description: "Documentation"},
		{Name: "python312", Conflicts: []string{"python313"}, Description: "Python 3.12"},
		{Name: "python313", Default: true, Conflicts: []string{"python312"}, Description: "Python 3.13"},
		{Name: "universal"}, // Base's own, for a port it can build for several architectures
	}, variants)
}
