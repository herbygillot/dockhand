package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
			// The link is only the symlink case's: Base reads what the
			// port's files directory holds, and a closed read is seen
			// whoever makes it (#110).
			link := filepath.Join(tree.Root(), "devel/host/files/link")
			if operation == "symlink" {
				require.NoError(t, os.Symlink(external, link))
			}
			paths := map[string]string{"open": external, "captured-open": captured, "symlink": link}
			// A probe that catches takes its absent branch where the
			// closed model answers, as on an empty Mac.
			body := fmt.Sprintf("if {[catch {open {%s} r} handle]} { set value absent } else { set value [read $handle]; close $handle }\n", paths[operation])
			switch operation {
			case "relative-stat":
				body = "set exists [file exists ../../../../../../tmp/dockhand-host-state]\n"
			case "source":
				body = fmt.Sprintf("catch {source {%s}}\n", external)
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
			// A captured read is no host state, as the verdict weighs it;
			// a host read is, at the moment it was made.
			inTree := operation == "captured-open" || operation == "captured-source"
			require.Equal(t, !inTree, hostState(t, got.Ports["host"], "PortSystem 1.0\nname host\nversion 1\n"+body), "%v", got.Ports["host"].Problems)
			if !inTree {
				require.True(t, got.Ports["host"].ModeledHostAccess, "%v", got.Ports["host"].Problems)
			}
		})
	}
}

// A port that reads the compiler Base picks for it, as gpsd reads
// configure.cxx with use_xcode yes, reads what Base asks the toolchain:
// which compilers exist. In a modelled context the facts table answers
// (docs/oracle.md, phase 5), so the port reads no host state and the
// compiler is the modelled release's. A release the table has no row for,
// Darwin 9, which no buildbot builds, is modelled all the same: what the
// table lacks is absent, closed, never this Mac's answer, and the closed
// read is seen (#110).
func TestCompilerDependentFetchInputComesFromTheTable(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	contents := "PortSystem 1.0\nname host\nversion 1\nuse_xcode yes\ndistfiles host-[file tail ${configure.cxx}].tar.gz\n"
	putFile(t, tree.Root(), "devel/host/Portfile", contents)
	bound, err := tree.Select(model.Target{Name: "host", Portfile: "devel/host/Portfile"})
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.False(t, hostState(t, got.Ports["host"], contents), "%v", got.Ports["host"].Problems)
	require.Equal(t, "host-clang++.tar.gz", got.Snapshot.Ports["host"].Options["distfiles"])
	got, err = e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "9", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.True(t, got.Ports["host"].ModeledHostAccess, "%v", got.Ports["host"].Problems)
	require.Positive(t, closedCalls(got.Ports["host"])["file executable"]+closedCalls(got.Ports["host"])["exec"], "%v", got.Ports["host"].Ledger)
	for _, entry := range got.Ports["host"].Ledger {
		require.NotEqual(t, "host-in-model", entry.Source, "no host answer: %+v", entry)
	}
	// With the plan taken before the code fingerprint's walk, what decides
	// the distfiles here is Base's own questions about the release's
	// developer tools, which reach the master and are closed there: weighed,
	// with nothing to explain them by, so the context still isn't
	// conclusive, and says why.
	require.Contains(t, strings.Join(got.Ports["host"].Problems, "\n"), "modeled context has no answer for what MacPorts asked of this release's developer tools (xcode-select")
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
	contents := "PortSystem 1.0\nname host\nversion 1\n" +
		"set built [clock format ${source_date_epoch} -format %Y-%m-%dT%H:%M:%SZ]\ndistfiles host-${source_date_epoch}-${built}.tar.gz\n"
	putFile(t, tree.Root(), "devel/host/Portfile", contents)
	when := time.Unix(1700000000, 0)
	require.NoError(t, os.Chtimes(filepath.Join(tree.Root(), "devel/host/Portfile"), when, when))
	bound, err := tree.Select(model.Target{Name: "host", Portfile: "devel/host/Portfile"})
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.False(t, hostState(t, got.Ports["host"], contents), "%v", got.Ports["host"].Problems)
	require.Equal(t, "host-1700000000-2023-11-14T22:13:20Z.tar.gz", got.Snapshot.Ports["host"].Options["distfiles"], "the Portfile's time, in UTC")

	// The same question of a directory outside the captured tree.
	putFile(t, tree.Root(), "devel/host/Portfile", "PortSystem 1.0\nname host\nversion 1\ncatch {exec git -c safe.directory=* -C "+t.TempDir()+" rev-parse --is-inside-work-tree}\n")
	got, err = e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	require.True(t, got.Ports["host"].ModeledHostAccess, "a directory outside the captured tree is the host's")
}

// Base's source_date_epoch over the captured tree is the same with git
// found, and asked whether the tree is a work tree, as with git not found:
// the claim that lets the verdict explain the closed lookup of git Base's
// get_source_date_epoch makes in a modelled context (architecture's call
// on #110's finding 1). The Mac's own context finds git and asks it, which
// the model answers as git does of a snapshot with no .git; the modelled
// one finds no git. Should a later Base make the two differ, this fails,
// and the explanation goes with it.
func TestSourceDateEpochIsTheSameWithGitAndWithout(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/host/Portfile", "PortSystem 1.0\nname host\nversion 1\ndistfiles host-${source_date_epoch}.tar.gz\n")
	when := time.Unix(1700000000, 0)
	require.NoError(t, os.Chtimes(filepath.Join(tree.Root(), "devel/host/Portfile"), when, when))
	bound, err := tree.Select(model.Target{Name: "host", Portfile: "devel/host/Portfile"})
	require.NoError(t, err)
	native, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Declarations: true})
	require.NoError(t, err)
	modelled, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: model.Platform{OS: "darwin", Version: "16", Architecture: "x86_64"}, Declarations: true})
	require.NoError(t, err)
	asked := func(observed macports.Observation, command, source string) bool {
		for _, entry := range observed.Ports["host"].Ledger {
			if entry.Command == command && entry.Source == source && entry.Subject == "git" {
				return true
			}
		}
		return false
	}
	require.True(t, asked(native, "exec", "table"), "the Mac's own context found git and asked it, answered as of a snapshot: %+v", native.Ports["host"].Ledger)
	require.True(t, asked(modelled, "findBinary", "closed"), "the modelled one found no git: %+v", modelled.Ports["host"].Ledger)
	require.Equal(t, "host-1700000000.tar.gz", native.Snapshot.Ports["host"].Options["distfiles"])
	require.Equal(t, native.Snapshot.Ports["host"].Options["distfiles"], modelled.Snapshot.Ports["host"].Options["distfiles"])
}

// A Portfile's own lookup of git that a fetch value rests on stays host
// state in a modelled context: only Base's source_date_epoch lookup, by
// its frame, is explained, never a lookup of git by its name.
func TestAPortfilesOwnLookupOfGitStaysHostState(t *testing.T) {
	t.Parallel()
	lines := "if {[catch {findBinary git}]} { distfiles none-${source_date_epoch}.tar.gz } else { distfiles git-${source_date_epoch}.tar.gz }\nlappend results [lindex ${distfiles} 0]"
	got, port := observedIn(t, monterey, nil, lines)
	require.Len(t, got, 1)
	require.Regexp(t, `^none-[0-9]+\.tar\.gz$`, got[0], "the empty Mac's answer")
	require.Contains(t, port.Problems, "modeled context depends on git, which findBinary looks for along this Mac's PATH")
	require.True(t, hostState(t, port, effects(lines)), "%v", port.Problems)
}

// Base's master keeps the clang compiler list in its parent interpreter,
// cached by the list's file alone, so a second context one process
// observes would get the first's; each modelled worker keeps its own
// (toolchain.tcl's install_lookups), so each context gets its own list,
// the same as a process of its own gives it.
func TestEachContextGetsItsOwnCompilerList(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/host/Portfile", "PortSystem 1.0\nname host\nversion 1\ndistfiles [join ${compiler.fallback} +].tar.gz\n")
	bound, err := tree.Select(model.Target{Name: "host", Portfile: "devel/host/Portfile"})
	require.NoError(t, err)
	platforms := []model.Platform{{OS: "darwin", Version: "25", Architecture: "arm64"}, {OS: "darwin", Version: "16", Architecture: "x86_64"}}
	alone := map[model.Platform]string{}
	for _, platform := range platforms {
		got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Platform: platform, Declarations: true})
		require.NoError(t, err)
		alone[platform] = got.Snapshot.Ports["host"].Options["distfiles"]
	}
	require.NotEqual(t, alone[platforms[0]], alone[platforms[1]], "the two contexts' lists differ")

	checked, err := bound.Tree.Select(bound.Target())
	require.NoError(t, err)
	session, runtime, err := e.start(t.Context(), checked.Tree)
	require.NoError(t, err)
	defer func() { require.NoError(t, session.Close()) }()
	for _, platform := range platforms {
		request := macports.ObservationRequest{Platform: platform, Declarations: true}
		got, err := evaluateIn(t.Context(), session, runtime, checked, bound, &request, false)
		require.NoError(t, err)
		require.Equal(t, alone[platform], got.Snapshot.Ports["host"].Options["distfiles"], "%+v, observed second in one process", platform)
	}
}

// A lookup Base's own default makes, for an option no fetch or checksum
// value reads, is explained like build.jobs's read, whoever wrote the
// option's default: build.cmd's make taken by a configure.args a PortGroup
// writes, as cmake's does, and use_bzip2's lbzip2 taken by extract.cmd
// (architecture's step 2 on #110's finding 1), and build.cmd's read by a
// Portfile in another phase's command, as abella's test.cmd is. One a
// fetch value reads, one the Portfile reads in an expression, or a
// Portfile's own lookup, pure-gen's build_getmaketype call among them,
// stays host state. So does one reaching a fetch value through another
// option, a proc or [set], one a Portfile's callback or a PortGroup's
// default makes, and one under a Portfile procedure named into portmain
// or portconfigure (architecture's points 1 to 4 on #110's finding 1).
func TestBasesOwnLookupForAPhaseOptionIsExplained(t *testing.T) {
	t.Parallel()
	group := func(root string) {
		putFile(t, root, "_resources/port1.0/group/makeargs-1.0.tcl", "default configure.args {-DCMAKE_MAKE_PROGRAM=${build.cmd}}\n")
	}
	hostDefault := func(root string) {
		putFile(t, root, "_resources/port1.0/group/hostdefault-1.0.tcl", "default build.cmd {[catch {findBinary hostonly}]}\n")
	}
	for _, c := range []struct {
		name, lines string
		setup       func(string)
		host        bool
		lookup      string
	}{
		{"build.cmd's make, taken by a PortGroup's configure.args", "PortGroup makeargs 1.0", group, false, "make, which findBinary"},
		{"extract.cmd's lbzip2", "use_bzip2 yes", nil, false, "lbzip2, which findBinary"},
		{"build.cmd's make, in the Portfile's test.cmd", "test.run yes\ntest.cmd ${build.cmd}", nil, false, "make, which findBinary"},
		{"build.cmd's make, read in the Portfile's expression", "build.args-append [string length ${build.cmd}]\nif {${build.cmd} eq {}} {build.target none}", nil, true, "make, which findBinary"},
		{"a Portfile's own call of Base's make lookup", "build.cmd [portbuild::build_getmaketype]", nil, true, "make, which findBinary"},
		{"configure.args read by a fetch value", "PortGroup makeargs 1.0\ndistfiles effects-[llength ${configure.args}].tar.gz", group, true, "make, which findBinary"},
		{"a Portfile's own lookup for configure.args", "catch {configure.args-append [findBinary hostonly]}", nil, true, "hostonly, which findBinary"},
		{"a Portfile's callback calling Base's make lookup", "proc mine {} { if {[catch {portbuild::build_getmaketype}]} {master_sites a} else {master_sites b} }\nport::register_callback mine", nil, true, "make, which findBinary"},
		{"a PortGroup's default looking for a program", "PortGroup hostdefault 1.0\nbuild.args ${build.cmd}", hostDefault, true, "hostonly, which findBinary"},
		{"build.cmd's make, through test.cmd to distfiles", "test.cmd ${build.cmd}\ndistfiles foo-[file tail ${test.cmd}].tgz", nil, true, "make, which findBinary"},
		{"build.cmd's make, through build.env to master_sites", "build.env MAKE=${build.cmd}\nmaster_sites http://x/${build.env}", nil, true, "make, which findBinary"},
		{"build.cmd's make, returned by a proc", "proc f {} {global build.cmd; return ${build.cmd}}\ndistfiles [f].tgz", nil, true, "make, which findBinary"},
		{"build.cmd's make, read by [set]", "distfiles foo-[file tail [set build.cmd]].tar.gz", nil, true, "make, which findBinary"},
		{"a Portfile procedure named like source_date_epoch's", "proc portmain::get_source_date_epoch_probe {} {return [catch {findBinary hostonly}]}\nif {[portmain::get_source_date_epoch_probe]} {distfiles a} else {distfiles b}", nil, true, "hostonly, which findBinary"},
		{"a Portfile procedure in portconfigure", "proc portconfigure::mine {} {return [catch {findBinary hostonly}]}\nif {[portconfigure::mine]} {distfiles a} else {distfiles b}", nil, true, "hostonly, which findBinary"},
	} {
		_, port := observedIn(t, monterey, c.setup, c.lines)
		require.True(t, slices.ContainsFunc(port.Problems, func(p string) bool { return strings.Contains(p, c.lookup) }), "%s: the lookup is closed: %v", c.name, port.Problems)
		require.Equal(t, c.host, hostState(t, port, effects(c.lines)), "%s: %v", c.name, port.Problems)
	}
}

// Base's own xcrun, asked for the SDK by configure.sdkroot's default
// where the table has no answer for the release (Darwin 9), is closed and
// explained: Base wrote the default and every frame below it is Base's
// library, and the option shapes the build alone (#110's finding 1,
// architecture's point 5).
func TestBasesOwnXcrunForTheSDKIsExplained(t *testing.T) {
	t.Parallel()
	leopard := model.Platform{OS: "darwin", Version: "9", Architecture: "i386"}
	lines := "build.args ${configure.sdkroot}"
	_, port := observedDescription(t, leopard, model.DeveloperToolsXcode, lines)
	xcrun := 0
	for _, declaration := range port.Declarations {
		if declaration.Command != "dockhand.host-access" || len(declaration.Values) < 4 || !strings.HasPrefix(declaration.Values[3], "exec xcrun ") {
			continue
		}
		xcrun++
		alone := port
		alone.Declarations, alone.Problems = []macports.Declaration{declaration}, []string{declaration.Values[0]}
		require.False(t, hostState(t, alone, effects(lines)), "%v %+v", declaration.Values, declaration.Frames)
	}
	require.Positive(t, xcrun, "Base asked xcrun: %v", port.Problems)
}
