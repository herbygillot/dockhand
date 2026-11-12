package eval

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// observedIn observes lines in a context, leaving a Tcl list in results
// that the port's description carries out, with setup given the tree's
// root before the evaluation.
func observedIn(t *testing.T, platform model.Platform, setup func(root string), lines string) ([]string, macports.PortObservation) {
	t.Helper()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	if setup != nil {
		setup(tree.Root())
	}
	putFile(t, tree.Root(), "devel/effects/Portfile", "PortSystem 1.0\nname effects\nversion 1\nset results {}\n"+lines+"\ndescription {*}$results\n")
	bound, err := tree.Select(model.Target{Name: "effects", Portfile: "devel/effects/Portfile"})
	require.NoError(t, err)
	got, err := e.Observe(t.Context(), bound, macports.ObservationRequest{Declarations: true, Platform: platform})
	require.NoError(t, err)
	values, errs := syntax.ListValues(got.Snapshot.Ports["effects"].Options["description"])
	require.Empty(t, errs)
	return values, got.Ports["effects"]
}

// closedCalls are the ledger's closed calls, each command and file
// subcommand with its count.
func closedCalls(port macports.PortObservation) map[string]int {
	closed := map[string]int{}
	for _, entry := range port.Ledger {
		if entry.Source == "closed" {
			closed[strings.TrimSpace(entry.Command+" "+entry.Subcommand)] += entry.Count
		}
	}
	return closed
}

var monterey = model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}

// hostState is the verdict on a modelled reading (observe.Tolerate):
// whether host state it read could reach what the Portfile declares.
// Base's own lookups for a build-only option's default, build.cmd's make,
// are closed in every modelled context since #110's finding 1 and
// explained there, so a test's "reads no host state" asks the verdict,
// not the raw flag (architecture's conditions on finding 1).
func hostState(t *testing.T, port macports.PortObservation, contents string) bool {
	t.Helper()
	// No PortGroup of the tree is read here: an empty tree stands for it.
	_, inconclusive := observe.Tolerate(t.Context(), port, []byte(contents), t.TempDir())
	return inconclusive
}

// effects is the Portfile observedIn and observedDescription write.
func effects(lines string) string {
	return "PortSystem 1.0\nname effects\nversion 1\nset results {}\n" + lines + "\ndescription {*}$results\n"
}

// A modelled context takes no host answer: a call that would reach the
// host is answered as an empty Mac answers it, file tests false and reads
// the no-such-file error, glob no match, source no file, and an admitted
// program "couldn't execute", each marked closed in the ledger, and seen
// by the verdict as host state (docs/designs/closed-model.md, #110).
func TestAModelledContextAnswersAsAnEmptyMac(t *testing.T) {
	t.Parallel()
	got, port := observedIn(t, monterey, nil, strings.Join([]string{
		"lappend results [file exists /etc/hosts] [file isfile /etc/hosts] [file readable /etc/hosts]",
		"lappend results [catch {file size /etc/hosts} m] $m",
		"lappend results [catch {open /etc/hosts} m] $m",
		"lappend results [llength [glob -nocomplain /etc/host*]] [catch {glob /etc/host*} m] $m",
		"lappend results [llength [glob -nocomplain -directory /etc host*]]",
		"lappend results [catch {source /etc/hosts} m] $m",
		"lappend results [catch {exec /usr/bin/true} m] $m",
		"if {[catch {exec /usr/bin/true}]} { lappend results absent } else { lappend results present }",
	}, "\n"))
	require.Equal(t, []string{
		"0", "0", "0",
		"1", `could not read "/etc/hosts": no such file or directory`,
		"1", `couldn't open "/etc/hosts": no such file or directory`,
		"0", "1", `no files matched glob pattern "/etc/host*"`,
		"0",
		"1", `couldn't read file "/etc/hosts": no such file or directory`,
		"1", `couldn't execute "/usr/bin/true": no such file or directory`,
		"absent",
	}, got)
	closed := closedCalls(port)
	for _, call := range []string{"file exists", "file isfile", "file size", "open", "glob", "source", "exec"} {
		require.Positive(t, closed[call], "%s is closed: %v", call, closed)
	}
	require.True(t, port.ModeledHostAccess, "the verdict sees what was closed")
	var owners []string
	for _, declaration := range port.Declarations {
		if declaration.Command == "dockhand.host-access" && len(declaration.Values) > 2 && declaration.Values[1] == "closed" {
			owners = append(owners, declaration.Values[2])
		}
	}
	require.Contains(t, owners, "portfile", "the Portfile's reads are its own")
}

// What the closed model leaves open: the captured tree and Tcl's library.
// A relative path is resolved before it's judged, and a link that leaves
// the tree, as the last step or an earlier one, is the host's; a glob is
// judged by its -directory or -path.
func TestTheClosedModelJudgesPathsOnce(t *testing.T) {
	t.Parallel()
	external := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(external, "data"), []byte("x"), 0o600))
	got, port := observedIn(t, monterey, func(root string) {
		putFile(t, root, "devel/effects/files/own", "x")
		require.NoError(t, os.Symlink(filepath.Join(external, "data"), filepath.Join(root, "devel/effects/files/last")))
		require.NoError(t, os.Symlink(external, filepath.Join(root, "devel/effects/files/earlier")))
	}, strings.Join([]string{
		"lappend results [file exists ${filespath}/own] [file exists [info library]/init.tcl]",
		"lappend results [file exists ../../../../../../../../../../etc/hosts]",
		"lappend results [file exists ${filespath}/last] [file exists ${filespath}/earlier/data]",
		"lappend results [llength [glob -nocomplain -directory ${filespath} own]] [llength [glob -nocomplain -path ${filespath}/ own]]",
		"lappend results [llength [glob -nocomplain -directory ${filespath}/earlier *]]",
	}, "\n"))
	require.Equal(t, []string{"1", "1", "0", "0", "0", "1", "1", "0"}, got)
	require.Equal(t, 3, closedCalls(port)["file exists"], "the relative path and both links: %v", port.Ledger)
}

// The Mac's own context reads the Mac: nothing is closed there.
func TestTheMacsOwnContextReadsTheMac(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" {
		t.Skip("the Mac's own context is a Mac's")
	}
	got, port := observedIn(t, model.Platform{}, nil, "lappend results [file exists /etc/hosts] [catch {exec /usr/bin/true}]")
	require.Equal(t, []string{"1", "0"}, got)
	require.Empty(t, closedCalls(port))
}

// Base's helpers that run programs are copied into a modelled worker with
// the caches the table fills: the table's clang version is answered, and
// one the table doesn't hold is answered empty, as an empty Mac answers
// it, never run (#106's host clang was one). Every alias the worker
// holds into the master is one this list names, read-only or answered by
// the dispatcher, so a new one fails here.
func TestAModelledWorkersHelpersAreItsOwn(t *testing.T) {
	t.Parallel()
	tools, err := macports.Toolchain(monterey, "")
	require.NoError(t, err)
	got, port := observedIn(t, monterey, nil, strings.Join([]string{
		"lappend results [get_compiler_version /usr/bin/clang $developer_dir] [get_tool_path clang]",
		"lappend results [get_compiler_version /opt/dockhand-absent/clang /opt/dockhand-absent]",
		"foreach alias [lsort [interp aliases {}]] { if {[catch {interp target {} $alias}]} { lappend into $alias } }",
		"lappend results $into",
	}, "\n"))
	// Under Xcode's developer_dir, /usr/bin/clang is Xcode's own (#213).
	clang := tools.Clang
	if tools.DeveloperDir != macports.CommandLineTools && tools.XcodeClang != "" {
		clang = tools.XcodeClang
	}
	require.Equal(t, []string{clang, "/usr/bin/clang", ""}, got[:3])
	for _, entry := range port.Ledger {
		require.NotEqual(t, "process", entry.Source, "nothing ran: %+v", entry)
		require.NotEqual(t, "master", entry.Subject, "nothing the master ran: %+v", entry)
	}
	into, errs := syntax.ListValues(got[3])
	require.Empty(t, errs)
	require.Equal(t, masterAliases, into, "an alias into the master is a read-only helper or the dispatcher's")
	require.NotContains(t, into, "get_compiler_version", "it is the worker's copy")
}

// masterAliases are what a modelled worker may call in the master, Base's
// worker aliases less the two helpers copied into it, sysctl among them
// though the dispatcher answers it first, closed in a modelled context
// since it reads this Mac's kernel (TestAModelledContextAsksNotThisMacsKernel): the registry's and
// the commands that write or reach the network, which the dispatcher
// answers or refuses before the alias is reached; Base's path and option
// arithmetic and its words to the person; and read-only helpers reading
// the ports tree (getportresourcepath and the getport* paths).
var masterAliases = []string{
	"_mport_archs", "_mport_supports_archs", "_mportkey", "_mportsearchpath", "_portnameactive", "binaryInPath",
	"curlwrap", "curlwrap_async", "curlwrap_async_cancel", "curlwrap_async_file_is_in_progress", "curlwrap_async_is_complete",
	"curlwrap_async_result", "curlwrap_async_show_progress", "dockhand_closed", "dockhand_refused", "findBinary", "find_tar_with_hfscompression",
	"get_actual_cxx_stdlib", "get_archive_sites_conf_values", "get_compatible_xcode_versions", "get_pingtime",
	"get_variant_description", "getdefaultportresourcepath", "getoption", "getportbuildpath", "getportdir", "getportlogpath",
	"getportresourcepath", "getportworkpath_from_buildpath", "getprotocol", "macports_create_thread", "macports_version",
	"mport_close", "mport_exec", "mport_info", "mport_lookup", "mport_open", "registry_activate", "registry_active",
	"registry_bulk_register_files", "registry_deactivate", "registry_deactivate_composite", "registry_exists",
	"registry_exists_for_name", "registry_file_registered", "registry_fileinfo_for_file", "registry_fileinfo_for_index",
	"registry_install", "registry_list_depends", "registry_new", "registry_open", "registry_port_registered",
	"registry_prop_retr", "registry_prop_store", "registry_register_deps", "registry_uninstall", "registry_write",
	"set_phase", "set_pingtime", "shellescape", "sysctl", "ui_any", "ui_channels", "ui_debug", "ui_error", "ui_info",
	"ui_msg", "ui_notice", "ui_notifications_append", "ui_prefix", "ui_progress_download", "ui_progress_generic", "ui_warn",
	"ui_warn_once", "verify_signature_openssl", "verify_signature_signify",
}

// Base's sysctl reads this Mac's kernel: a Portfile asking it, as gtk3
// asks hw.cpu64bit_capable and atlas hw.cpufamily, takes the closed answer
// in a modelled context, an error a catch takes as absent, and the
// verdict sees it; the Mac's own context reads the Mac (#110).
func TestAModelledContextAsksNotThisMacsKernel(t *testing.T) {
	t.Parallel()
	got, port := observedIn(t, monterey, nil, "lappend results [catch {sysctl hw.memsize} m]")
	require.Equal(t, []string{"1"}, got)
	require.Positive(t, closedCalls(port)["sysctl"], "%v", port.Ledger)
	require.True(t, port.ModeledHostAccess)
	if runtime.GOOS == "darwin" {
		got, _ = observedIn(t, model.Platform{}, nil, "lappend results [catch {sysctl hw.memsize} m]")
		require.Equal(t, []string{"0"}, got)
	}
}

// An evaluation a closed answer ends, a Portfile running a program
// unguarded as apache-arrow's runs echo, is a context the model has no
// answer for, named by the call, never "couldn't execute" said as
// dockhand's own failure (#110).
func TestAnEvaluationAClosedAnswerEndsNamesTheCall(t *testing.T) {
	t.Parallel()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/effects/Portfile", "PortSystem 1.0\nname effects\nversion 1\nset said [exec echo hi]\n")
	bound, err := tree.Select(model.Target{Name: "effects", Portfile: "devel/effects/Portfile"})
	require.NoError(t, err)
	_, err = e.Observe(t.Context(), bound, macports.ObservationRequest{Declarations: true, Platform: monterey})
	var unanswered *macports.UnansweredError
	require.ErrorAs(t, err, &unanswered)
	require.Equal(t, "exec echo hi", unanswered.Call)
	require.ErrorIs(t, err, macports.ErrUnanswered)
}

// A file subcommand abbreviated as Tcl takes it, file exi or file isf, is
// the read it names, and closed as it is (Codex's review of #110 and #125,
// finding 2): it read the host, judged as reading nothing.
func TestAnAbbreviatedFileReadIsClosed(t *testing.T) {
	t.Parallel()
	got, port := observedIn(t, monterey, nil, `lappend results [file exi /etc/hosts] [file isf /etc/hosts]`)
	require.Equal(t, []string{"0", "0"}, got)
	require.True(t, port.ModeledHostAccess)
	require.Equal(t, 1, closedCalls(port)["file exists"])
	require.Equal(t, 1, closedCalls(port)["file isfile"])
}

// A glob whose wildcard matches a link out of the tree is the host's
// read: a link at any step leaving the roots makes it so (Codex's review
// of #110 and #125, finding 3), and an empty Mac answers it with no match,
// closed.
func TestAGlobThroughALinkOutOfTheTreeIsClosed(t *testing.T) {
	t.Parallel()
	external := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(external, "hostdata"), []byte("host"), 0o600))
	got, port := observedIn(t, monterey, func(root string) {
		putFile(t, root, "devel/effects/files/own/hostdata", "x")
		require.NoError(t, os.Symlink(external, filepath.Join(root, "devel/effects/files/escape")))
	}, `lappend results [llength [glob -nocomplain ${filespath}/*/hostdata]] [llength [glob -nocomplain ${filespath}/own/*]]`)
	require.Equal(t, []string{"0", "1"}, got, "the glob through the link is closed; one of the tree alone reads it")
	require.True(t, port.ModeledHostAccess)
	require.Equal(t, 1, closedCalls(port)["glob"])
}

// A glob joining its patterns, with -directory or -path or neither, takes
// the prefix once on the joined pattern, as Tcl does, so the check sees
// the pattern Tcl runs through the link and closes it (the outside
// reader's point 6 on #110): with each pattern prefixed and then joined it
// saw one that matches nothing, and the host's answer came back.
func TestAJoinedGlobThroughALinkOutOfTheTreeIsClosed(t *testing.T) {
	t.Parallel()
	external := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(external, "hostdata"), []byte("host"), 0o600))
	setup := func(root string) {
		putFile(t, root, "devel/effects/files/own/hostdata", "x")
		require.NoError(t, os.Symlink(external, filepath.Join(root, "devel/effects/files/escape")))
	}
	for _, line := range []string{
		`lappend results [llength [glob -nocomplain -join ${filespath} * hostdata]]`,
		`lappend results [llength [glob -nocomplain -join -directory ${filespath} * hostdata]]`,
		`lappend results [llength [glob -nocomplain -join -path ${filespath}/ * hostdata]]`,
		`lappend results [llength [glob -nocomplain -directory ${filespath} -tails */hostdata]]`,
	} {
		got, port := observedIn(t, monterey, setup, line)
		require.Equal(t, []string{"0"}, got, "%s: the glob through the link is closed", line)
		require.Positive(t, closedCalls(port)["glob"], "%s", line)
		require.True(t, hostState(t, port, effects(line)), "%s", line)
	}
}

// A glob the fresh installation filters returns nothing the host alone
// supplied (Codex's review of #110 and #125, finding 4): /etc/hosts came
// back beside the prefix's programs it kept.
func TestAFilteredGlobKeepsNothingOfTheHost(t *testing.T) {
	t.Parallel()
	got, port := observedIn(t, monterey, nil, `lappend results [expr {[lsearch -exact [glob -nocomplain /etc/hosts ${prefix}/bin/*] /etc/hosts] >= 0}]`)
	require.Equal(t, []string{"0"}, got)
	require.True(t, port.ModeledHostAccess)
	require.NotEmpty(t, closedCalls(port))
}

// Setting up the native context after a modelled one in the same
// interpreter restores what the modelled setup changed (Codex's review of
// #110 and #125, finding 7): native, modelled, native.
func TestTheNativeContextIsRestoredAfterAModelledOne(t *testing.T) {
	t.Parallel()
	_, tcl := tclSession(t)
	native := tcl("list $::macports::os_major $::macports::build_arch $::macports::macos_version")
	overrides, err := macports.ModelVariables(monterey, "")
	require.NoError(t, err)
	tools, err := macports.ToolchainAnswers(monterey, "")
	require.NoError(t, err)
	tcl("::dockhand::observation_setup {" + overrides + "} 0 {} {" + tools + "}")
	modelled := tcl("list $::macports::os_major $::macports::build_arch $::macports::macos_version")
	require.Regexp(t, `^21 arm64 12\.`, modelled)
	tcl("::dockhand::observation_setup {} 0 {} {}")
	require.Equal(t, native, tcl("list $::macports::os_major $::macports::build_arch $::macports::macos_version"))
	require.Equal(t, "0", tcl("set ::dockhand::modeled"))
	tcl("::dockhand::observation_setup {" + overrides + "} 0 {} {" + tools + "}")
	require.Equal(t, modelled, tcl("list $::macports::os_major $::macports::build_arch $::macports::macos_version"), "and a modelled one after it is modelled again")
}

// The native context set up after a modelled one has Base's compiler
// caches and developer directory back as they were (the outside reader's
// control beside point 6 on #110).
func TestTheNativeCachesAreRestoredAfterAModelledOne(t *testing.T) {
	t.Parallel()
	_, tcl := tclSession(t)
	tcl(`set ::macports::compiler_version_cache {versions sentinel}; set ::macports::tool_path_cache {clang native-clang}`)
	read := `list $::macports::compiler_version_cache $::macports::tool_path_cache $::macports::developer_dir`
	native := tcl(read)
	overrides, err := macports.ModelVariables(monterey, "")
	require.NoError(t, err)
	tools, err := macports.ToolchainAnswers(monterey, "")
	require.NoError(t, err)
	tcl("::dockhand::observation_setup {" + overrides + "} 0 {} {" + tools + "}")
	tcl("::dockhand::observation_setup {} 0 {} {}")
	require.Equal(t, native, tcl(read))
}

// Base's lookups of a program along PATH and of a dependency's file along
// directories, which the fresh installation answers in the worker, take
// nothing of this Mac's outside the prefix in a modelled context: the
// table's answer where it has one, else the empty Mac's, each lookup
// closed once, naming what it looked for and which lookup asked (Codex's
// review of #110 and #125, finding 1, as architecture chose it). They
// found /usr/bin/true and /etc/hosts, recorded as the fresh installation's.
func TestBaseLookupsFindNothingOfTheHostInAModelledContext(t *testing.T) {
	t.Parallel()
	got, port := observedIn(t, monterey, nil, `lappend results [catch {findBinary true /usr/bin/true} m] [catch {binaryInPath true} m] [_mportsearchpath hosts /etc 0 1] [findBinary clang]`)
	require.Equal(t, []string{"1", "1", "", "/usr/bin/clang"}, got, "neither lookup finds a host executable, nor the dependency search a host file; the table answers for clang")
	closed := map[string]int{}
	for _, entry := range port.Ledger {
		if entry.Source == "closed" && entry.Subject != "make" {
			closed[entry.Command+" "+entry.Subject] += entry.Count
		}
	}
	require.Equal(t, map[string]int{"findBinary true": 1, "binaryInPath true": 1, "_mportsearchpath hosts": 1}, closed, "each lookup that found nothing once, findBinary's own binaryInPath in it; the table's clang is no host read")
	require.True(t, port.ModeledHostAccess)
	for _, problem := range []string{
		"modeled context depends on true, which findBinary looks for along this Mac's PATH",
		"modeled context depends on true, which binaryInPath looks for along this Mac's PATH",
		"modeled context depends on hosts, which _mportsearchpath looks for in this Mac's /etc",
	} {
		require.Contains(t, port.Problems, problem)
	}
}
