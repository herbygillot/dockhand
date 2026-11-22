package observe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// baseFixture is a Base library holding the defaults these tests' frames
// evaluate, as Base's port1.0 writes them, and the frames' shapes as the
// evaluator records them (TestBasesOwnLookupForAPhaseOptionIsExplained
// observes the same chains live).
type baseFixture struct {
	lib string
}

func newBaseFixture(t *testing.T) baseFixture {
	t.Helper()
	lib := filepath.Join(t.TempDir(), "libexec", "macports", "lib")
	for name, body := range map[string]string{
		"port1.0/portbuild.tcl":     "default build.cmd {[portbuild::build_getmaketype]}\ndefault build.jobs {[portbuild::build_getjobs]}\n",
		"port1.0/portmain.tcl":      "options prefix name version revision epoch \\\n    categories maintainers\ndefault source_date_epoch {[portmain::get_source_date_epoch]}\n",
		"port1.0/portfetch.tcl":     "options master_sites patch_sites distfiles patchfiles \\\n    dist_subdir checksums\n",
		"port1.0/portconfigure.tcl": "namespace eval portconfigure {}\nforeach _portconfigure_tool {cc objc cpp f77 f90 fc javac} { default configure.$_portconfigure_tool \"\\[portconfigure::configure_get_compiler $_portconfigure_tool\\]\" }\ndefault configure.developer_dir {[portconfigure::configure_get_developer_dir]}\ndefault configure.sdkroot {[portconfigure::configure_get_sdkroot ${configure.sdk_version}]}\ndefault configure.compiler {[portconfigure::configure_get_default_compiler]}\ndefault compiler.fallback {[portconfigure::get_compiler_fallback]}\n",
		"port1.0/portutil.tcl":      "proc default_check {optionName index op} {}\n",
	} {
		path := filepath.Join(lib, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	return baseFixture{lib: lib}
}

func (b baseFixture) file(name string) string { return filepath.Join(b.lib, filepath.FromSlash(name)) }

func frame(command string) macports.SourceFrame {
	return macports.SourceFrame{Line: 1, Command: command}
}

// substitution is Base's default_check evaluating option's default text.
func (b baseFixture) substitution(option, text string) []macports.SourceFrame {
	return []macports.SourceFrame{
		frame("default_check " + option + " {} read"),
		{File: b.file("port1.0/portutil.tcl"), Line: 520, Command: "uplevel #0 [list subst [dict get $option_defaults $optionName]]"},
		frame("subst {" + text + "}"),
	}
}

// caught is Base's catch running script: signalcatch's uplevel into the
// builtin.
func (b baseFixture) caught(script string) []macports.SourceFrame {
	return []macports.SourceFrame{
		{File: b.file("mpcommon1.0/signalcatch.tcl"), Line: 15, Command: "uplevel 1 [list builtin_catch {*}$args]"},
		frame("builtin_catch {" + script + "}"),
	}
}

// getmaketype is portbuild::build_getmaketype finding make, as Base's
// portbuild.tcl:94 does inside macports_try.
func (b baseFixture) getmaketype() []macports.SourceFrame {
	return []macports.SourceFrame{
		frame("portbuild::build_getmaketype"),
		{File: b.file("port1.0/portbuild.tcl"), Line: 94, Command: "macports_try -pass_signal {\n        if {${build.type} eq \"default\"} {\n            return [findBinary make $portutil::autoconf::make_path]"},
		{File: b.file("macports1.0/macports_util.tcl"), Line: 367, Command: "uplevel 1 [list ::builtin_catch $trybody $EMVAR $OPTVAR]"},
		frame("::builtin_catch {\n        if {${build.type} eq \"default\"} {\n            return [findBinary make $portutil::autoconf::make_path]"),
		{Line: 3, Command: "findBinary make $portutil::autoconf::make_path"},
	}
}

// dispatched is the dispatcher's own frames under a closed call.
func (b baseFixture) dispatched() []macports.SourceFrame {
	return append([]macports.SourceFrame{{Line: 24, Command: "fresh $name $args"}, {Line: 9, Command: "catch {$name {*}$arguments} result options"}},
		append(b.caught("$name {*}$arguments"), frame("$name {*}$arguments"))...)
}

func (b baseFixture) lookup(program, owner string, frames ...[]macports.SourceFrame) macports.Declaration {
	var all []macports.SourceFrame
	for _, part := range frames {
		all = append(all, part...)
	}
	all = append(all, b.dispatched()...)
	return macports.Declaration{Command: "dockhand.host-access", Values: []string{"modeled context depends on " + program + ", which findBinary looks for along this Mac's PATH", "closed", owner, "findBinary " + program}, Frames: all}
}

func (b baseFixture) tolerate(t *testing.T, contents string, d macports.Declaration) bool {
	t.Helper()
	return b.tolerateAt(t, t.TempDir(), contents, d)
}

func (b baseFixture) tolerateAt(t *testing.T, root, contents string, d macports.Declaration) bool {
	t.Helper()
	port := macports.PortObservation{ModeledHostAccess: true, Problems: []string{d.Values[0]}, Declarations: []macports.Declaration{d}, BaseLibrary: b.lib, MachineReadExperiment: &macports.MachineReadExperiment{Outcome: "same", Varied: []int{0}}}
	if !strings.HasPrefix(d.Values[3], "findBinary ") {
		port.MachineReadExperiment = nil
	}
	_, inconclusive := Tolerate(t.Context(), port, []byte("PortSystem 1.0\nname x\nversion 1\n"+contents+"\n"), root)
	return inconclusive
}

func (b baseFixture) toolchainRead(frames ...macports.SourceFrame) macports.Declaration {
	return macports.Declaration{
		Command: "dockhand.host-access",
		Values:  []string{"modeled context depends on filesystem state outside the captured ports tree", "closed", "base", "file exists /usr/lib/libxcselect.dylib"},
		Frames:  append(frames, macports.SourceFrame{File: b.file("port1.0/portconfigure.tcl"), Line: 700, Command: "file exists /usr/lib/libxcselect.dylib"}),
	}
}

func (b baseFixture) configureCCDefault() []macports.SourceFrame {
	return []macports.SourceFrame{
		frame("default_check configure.cc {} read"),
		{File: b.file("port1.0/portutil.tcl"), Line: 520, Command: "uplevel #0 [list subst [dict get $option_defaults $optionName]]"},
		frame("subst {[portconfigure::configure_get_compiler cc]}"),
		frame("portconfigure::configure_get_compiler cc"),
		frame("default_check configure.developer_dir {} read"),
		{File: b.file("port1.0/portutil.tcl"), Line: 520, Command: "uplevel #0 [list subst [dict get $option_defaults $optionName]]"},
		frame("subst {[portconfigure::configure_get_developer_dir]}"),
		frame("portconfigure::configure_get_developer_dir"),
	}
}

func TestToolchainProbeRequiresBaseToWriteTheOutermostCall(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	baseCall := b.toolchainRead(b.configureCCDefault()...)
	portfileCall := b.toolchainRead(
		frame("source "+thePortfile),
		macports.SourceFrame{File: thePortfile, Line: 4, Command: "distfiles x-[file tail [portconfigure::configure_get_sdkroot 10.6]].tar.gz"},
		frame("portconfigure::configure_get_sdkroot 10.6"),
	)
	portfileDefault := b.toolchainRead(
		frame("default_check fixture.sdk {} read"),
		macports.SourceFrame{File: b.file("port1.0/portutil.tcl"), Line: 520, Command: "uplevel #0 [list subst [dict get $option_defaults $optionName]]"},
		frame("subst {[portconfigure::configure_get_sdkroot 10.6]}"),
		frame("portconfigure::configure_get_sdkroot 10.6"),
	)

	require.False(t, b.tolerate(t, "build.args ${configure.cc}", baseCall), "Base's nested defaults explain the compiler probe")
	t.Run("Portfile call", func(t *testing.T) {
		require.True(t, b.tolerate(t, "distfiles x-[file tail [portconfigure::configure_get_sdkroot 10.6]].tar.gz", portfileCall), "a call written by the Portfile stays inconclusive")
	})
	t.Run("Portfile lazy default", func(t *testing.T) {
		require.True(t, b.tolerate(t, "options fixture.sdk\ndefault fixture.sdk {[portconfigure::configure_get_sdkroot 10.6]}\nbuild.args ${fixture.sdk}", portfileDefault), "a Portfile's lazy default is not Base's")
	})
}

func TestDefaultDeclaredInsideBaseForeachBelongsToBase(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	frames := []macports.SourceFrame{
		frame("default_check configure.cc {} read"),
		{File: b.file("port1.0/portutil.tcl"), Line: 520, Command: "uplevel #0 [list subst [dict get $option_defaults $optionName]]"},
		frame("subst {[portconfigure::configure_get_compiler cc]}"),
		frame("portconfigure::configure_get_compiler cc"),
		{File: b.file("port1.0/portconfigure.tcl"), Line: 1378, Command: "set v [compiler.command_line_tools_version $c]"},
	}
	require.True(t, newBaseCode(b.lib).defaultIsBases(frames, len(frames)), "Base declares configure.cc in its foreach-generated defaults")
}

func TestToolchainProbeStillUsesToolchainReadGate(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	declaration := b.toolchainRead(b.configureCCDefault()...)
	for _, tc := range []struct {
		name     string
		contents string
		want     bool
	}{
		{"Gyoto build-only condition", `if {[string match *clang* ${configure.cxx}]} { build.args CC=${configure.cc} }`, false},
		{"condition writes distfiles", `if {[string match *clang* ${configure.cxx}]} { distfiles other.tar.gz }`, true},
		{"condition writes master_sites", `if {[string match *clang* ${configure.cxx}]} { master_sites https://example.invalid }`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, b.tolerate(t, tc.contents, declaration))
		})
	}
}

func TestPortGroupDirectToolchainCallNeedsABuildOnlyRead(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	for _, tc := range []struct {
		name         string
		group        string
		portfile     string
		inconclusive bool
	}{
		{
			name:     "build-only",
			group:    "if {[string match *clang* ${configure.cxx}]} {\n    build.args CC=[portconfigure::configure_get_compiler cc]\n}\n",
			portfile: "PortGroup hostcall 1.0",
		},
		{
			name:         "fetch input",
			group:        "distfiles x-[file tail [portconfigure::configure_get_compiler cc]].tar.gz\n",
			portfile:     "PortGroup hostcall 1.0",
			inconclusive: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			groupPath := filepath.Join(root, "_resources", "port1.0", "group", "hostcall-1.0.tcl")
			require.NoError(t, os.MkdirAll(filepath.Dir(groupPath), 0o755))
			require.NoError(t, os.WriteFile(groupPath, []byte(tc.group), 0o644))
			line := 1
			if strings.HasPrefix(tc.group, "if ") {
				line = 2
			}
			declaration := b.toolchainRead(
				macports.SourceFrame{File: groupPath, Line: 1, Command: strings.TrimSpace(strings.Split(tc.group, "\n")[0])},
				macports.SourceFrame{File: groupPath, Line: line, Command: "portconfigure::configure_get_compiler cc"},
			)
			require.Equal(t, tc.inconclusive, b.tolerateAt(t, root, tc.portfile, declaration))
		})
	}
}

const thePortfile = "/tree/devel/x/Portfile"

// These ownership fixtures supply equal, varied summaries; live product
// fixtures exercise the actual comparison.
// A lookup made under a default is Base's only where Base wrote the
// default being evaluated, by Base's own source, and nothing below it is
// a Portfile's or PortGroup's code: a default a Portfile or PortGroup
// wrote that looks for a program itself has no file frame, and Base's
// default_check and catch are on its stack (#110 finding 1,
// architecture's point 2).
func TestALookupInADefaultBaseDidntWriteIsTheWritersOwn(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	own := b.lookup("hostonly", "base", b.substitution("build.cmd", "[catch {findBinary hostonly}]"),
		[]macports.SourceFrame{frame("catch {findBinary hostonly}")}, b.caught("findBinary hostonly"), []macports.SourceFrame{frame("findBinary hostonly")})
	require.True(t, b.tolerate(t, "PortGroup hostdefault 1.0\nbuild.args ${build.cmd}", own), "a PortGroup's default looking for a program")

	bare := b.lookup("hostonly", "base", b.substitution("build.cmd", "[findBinary hostonly]"), []macports.SourceFrame{frame("findBinary hostonly")})
	require.True(t, b.tolerate(t, "PortGroup hostdefault 1.0\nbuild.args ${build.cmd}", bare), "one that doesn't catch")

	// A default naming a procedure the Portfile defined, which calls
	// Base's lookup and writes a fetch field beside it.
	wrapped := b.lookup("make", "base", b.substitution("build.cmd", "[mine]"),
		[]macports.SourceFrame{frame("mine"), {File: thePortfile, Line: 4, Command: "catch {portbuild::build_getmaketype}"}}, b.caught("portbuild::build_getmaketype"), b.getmaketype())
	require.True(t, b.tolerate(t, "PortGroup hostdefault 1.0\nbuild.args ${build.cmd}", wrapped), "a Portfile procedure under the default")

	// Base's own procedure overridden by the Portfile: Base's default text,
	// the Portfile's code.
	overridden := b.lookup("hostonly", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"),
		[]macports.SourceFrame{frame("portbuild::build_getmaketype"), {File: thePortfile, Line: 4, Command: "findBinary hostonly"}})
	require.True(t, b.tolerate(t, "build.args ${build.cmd}", overridden), "Base's default calling the Portfile's procedure")

	base := b.lookup("make", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	require.False(t, b.tolerate(t, "build.args ${build.cmd}", base), "Base's own default")
	cmake := b.lookup("make", "base", b.substitution("configure.args", "-DCMAKE_MAKE_PROGRAM=${build.cmd}"), b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	require.False(t, b.tolerate(t, "", cmake), "Base's default under a PortGroup's, as cmake's configure.args reads build.cmd")
}

// A lookup in a callback is Base's only where every frame below the
// callback is Base's library: a callback a Portfile or PortGroup
// registered that calls Base's lookup is its own, as pure-gen's direct
// call is, whoever the evaluator named its owner (#110 finding 1,
// architecture's point 1). A frame is Base's by its file's path under the
// library the evaluator loaded Base from, never by a substring of it
// (point 4).
func TestALookupInACallbackIsBasesOnlyBelowBasesCode(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	callback := []macports.SourceFrame{{Command: "port::run_callbacks"}, {Line: 5, Command: "${callback}"}}
	mine := "proc mine {} { if {[catch {portbuild::build_getmaketype}]} {master_sites a} else {master_sites b} }\nport::register_callback mine"
	portfiles := []macports.SourceFrame{{File: thePortfile, Line: 4, Command: "catch {portbuild::build_getmaketype}"}}
	require.True(t, b.tolerate(t, mine, b.lookup("make", "portfile", callback, portfiles, b.caught("portbuild::build_getmaketype"), b.getmaketype())), "the Portfile's callback")
	require.True(t, b.tolerate(t, mine, b.lookup("make", "base", callback, portfiles, b.caught("portbuild::build_getmaketype"), b.getmaketype())), "the Portfile's callback, whatever its owner says")
	groups := []macports.SourceFrame{{File: "/tree/_resources/port1.0/group/mine-1.0.tcl", Line: 4, Command: "catch {portbuild::build_getmaketype}"}}
	require.True(t, b.tolerate(t, "", b.lookup("make", "portgroup", callback, groups, b.caught("portbuild::build_getmaketype"), b.getmaketype())), "a PortGroup's callback")

	lbzip2 := func(file string) macports.Declaration {
		return b.lookup("lbzip2", "base", callback, []macports.SourceFrame{{File: file, Line: 175, Command: "catch {findBinary lbzip2}"}}, b.caught("findBinary lbzip2"), []macports.SourceFrame{frame("findBinary lbzip2")})
	}
	require.False(t, b.tolerate(t, "use_bzip2 yes", lbzip2(b.file("port1.0/portextract.tcl"))), "Base's add_extract_deps")
	require.True(t, b.tolerate(t, "use_bzip2 yes", lbzip2("/srv/libexec/macports/lib/port1.0/portextract.tcl")), "a file whose path only contains a Base library's")
	require.True(t, b.tolerate(t, "use_bzip2 yes", lbzip2(filepath.Join(b.lib+"-tree", "port1.0", "portextract.tcl"))), "a directory beside Base's library sharing its prefix")

	direct := b.lookup("make", "portfile", []macports.SourceFrame{frame("source " + thePortfile), {File: thePortfile, Line: 5, Command: "portbuild::build_getmaketype"}}, b.getmaketype()[1:])
	require.True(t, b.tolerate(t, "build.cmd [portbuild::build_getmaketype]", direct), "pure-gen's own call")
}

// Base's get_source_date_epoch and compiler selection are Base's by the
// exact procedure, whose body is Base's library: a Portfile procedure
// named into portmain or portconfigure, or one overriding Base's, is the
// Portfile's own (#110 finding 1, architecture's point 4). Base's own
// xcrun for configure.sdkroot's default stays explained.
func TestBasesNamedProbesAreBasesByTheirCode(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	git := func(frames ...macports.SourceFrame) macports.Declaration {
		return b.lookup("git", "base", b.substitution("source_date_epoch", "[portmain::get_source_date_epoch]"), frames, b.caught("findBinary git"), []macports.SourceFrame{frame("findBinary git")})
	}
	require.False(t, b.tolerate(t, "distfiles x-${source_date_epoch}.tgz", git(frame("portmain::get_source_date_epoch"), macports.SourceFrame{File: b.file("port1.0/portmain.tcl"), Line: 180, Command: "catch {findBinary git} git"})), "Base's source_date_epoch")
	require.True(t, b.tolerate(t, "distfiles x-${source_date_epoch}.tgz", git(frame("portmain::get_source_date_epoch"), macports.SourceFrame{File: thePortfile, Line: 3, Command: "catch {findBinary git} git"})), "the Portfile's override of it")

	imitated := func(name string) macports.Declaration {
		return b.lookup("hostonly", "portfile", []macports.SourceFrame{frame("source " + thePortfile), {File: thePortfile, Line: 6, Command: "if {[" + name + "]} {distfiles a} else {distfiles b}"}, frame(name), {File: thePortfile, Line: 5, Command: "catch {findBinary hostonly}"}},
			b.caught("findBinary hostonly"), []macports.SourceFrame{frame("findBinary hostonly")})
	}
	require.True(t, b.tolerate(t, "proc portmain::get_source_date_epoch_probe {} {return [catch {findBinary hostonly}]}\nif {[portmain::get_source_date_epoch_probe]} {distfiles a} else {distfiles b}", imitated("portmain::get_source_date_epoch_probe")), "a name sharing the prefix")
	require.True(t, b.tolerate(t, "proc portconfigure::mine {} {return [catch {findBinary hostonly}]}\nif {[portconfigure::mine]} {distfiles a} else {distfiles b}", imitated("portconfigure::mine")), "a Portfile procedure in portconfigure")

	xcrun := func(file string) macports.Declaration {
		call := "exec xcrun --sdk macosx --show-sdk-path 2> /dev/null"
		frames := append(b.substitution("configure.sdkroot", "[portconfigure::configure_get_sdkroot ${configure.sdk_version}]"),
			frame("portconfigure::configure_get_sdkroot ${configure.sdk_version}"), macports.SourceFrame{File: file, Line: 699, Command: "catch {" + call + "} sdk"})
		frames = append(append(frames, b.caught(call)...), frame(call))
		return macports.Declaration{Command: "dockhand.host-access", Values: []string{"modeled context depends on a host process executed while evaluating the Portfile", "closed", "base", call}, Frames: frames}
	}
	require.False(t, b.tolerate(t, "build.args ${configure.sdkroot}", xcrun(b.file("port1.0/portconfigure.tcl"))), "the existing toolchain rule still explains this unvaried exec")
	require.True(t, b.tolerate(t, "build.args ${configure.sdkroot}", xcrun(thePortfile)), "a Portfile's xcrun under a portconfigure name")

	// Base's universal_setup choosing the compiler on Darwin 10: Base's
	// procedure reads options whose defaults Base wrote, so the first
	// file below it is default_check's substitution, and the read is in
	// SDK selection three defaults down.
	selection := func(fallback string) macports.Declaration {
		frames := []macports.SourceFrame{frame("universal_setup")}
		frames = append(frames, b.substitution("configure.compiler", "[portconfigure::configure_get_default_compiler]")...)
		frames = append(frames, frame("portconfigure::configure_get_default_compiler"))
		frames = append(frames, b.substitution("compiler.fallback", fallback)...)
		frames = append(frames, frame("portconfigure::get_compiler_fallback"), macports.SourceFrame{File: b.file("port1.0/portconfigure.tcl"), Line: 1367, Command: "portconfigure::get_apple_compilers_xcode_version"})
		frames = append(frames, b.substitution("configure.sdkroot", "[portconfigure::configure_get_sdkroot ${configure.sdk_version}]")...)
		frames = append(frames, frame("portconfigure::configure_get_sdkroot ${configure.sdk_version}"), macports.SourceFrame{File: b.file("port1.0/portconfigure.tcl"), Line: 571, Command: "file exists /usr/include/sys/cdefs.h"})
		return macports.Declaration{Command: "dockhand.host-access", Values: []string{"modeled context depends on filesystem state outside the captured ports tree", "closed", "base", "file exists /usr/include/sys/cdefs.h"}, Frames: frames}
	}
	require.False(t, b.tolerate(t, "", selection("[portconfigure::get_compiler_fallback]")), "Base's compiler selection through its own defaults")
	require.True(t, b.tolerate(t, "", selection("[catch {portconfigure::get_compiler_fallback}]")), "through a default Base didn't write")
}

func TestPatchfilesIsInTheActedOptionList(t *testing.T) {
	t.Parallel()
	require.True(t, ActedOption("patchfiles"))
	require.True(t, ActedOption("patchfiles-append"))
	require.False(t, benignSink("patchfiles"), "patchfiles is acted on, so the older rules no longer take it as a benign sink")
}
