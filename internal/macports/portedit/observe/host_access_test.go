package observe

import (
	"os"
	"path/filepath"
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
		"port1.0/portmain.tcl":      "default source_date_epoch {[portmain::get_source_date_epoch]}\n",
		"port1.0/portconfigure.tcl": "namespace eval portconfigure {}\ndefault configure.sdkroot {[portconfigure::configure_get_sdkroot ${configure.sdk_version}]}\n",
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
	port := macports.PortObservation{ModeledHostAccess: true, Problems: []string{d.Values[0]}, Declarations: []macports.Declaration{d}, BaseLibrary: b.lib}
	_, inconclusive := Tolerate(t.Context(), port, []byte("PortSystem 1.0\nname x\nversion 1\n"+contents+"\n"), t.TempDir())
	return inconclusive
}

const thePortfile = "/tree/devel/x/Portfile"

// A Base lookup's value is followed wherever the Portfile takes it: an
// option set from it is judged as it is, to a fixed point, and a read the
// walk can't place, by [set], in a proc's body, in an expression, a
// substitution or a script it doesn't walk, leaves the context
// inconclusive (#110 finding 1, architecture's points 3 and 5). Only reads
// in build positions, of options no fetch value reads, are explained:
// abella's test.cmd ${build.cmd}, and patchfiles, a benign sink by design.
func TestABaseLookupsValueIsFollowedToEveryFetchValue(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	getmake := b.lookup("make", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	for _, c := range []struct {
		name, contents string
		host           bool
	}{
		{"no read", "", false},
		{"abella's test.cmd", "test.run yes\ntest.cmd ${build.cmd}", false},
		{"test.cmd, read by the build", "test.cmd ${build.cmd}\nbuild.args ${test.cmd}", false},
		{"in a phase body", "post-destroot {\n    system \"${build.cmd} install\"\n}", false},
		{"patchfiles, a benign sink", "patchfiles-append patch-[file tail ${build.cmd}].diff", false},
		{"a comment naming it", "# build.cmd is make\nbuild.args ${build.cmd}", false},
		{"distfiles", "distfiles foo-[file tail ${build.cmd}].tar.gz", true},
		{"[set]", "distfiles foo-[file tail [set build.cmd]].tar.gz", true},
		{"subst", "distfiles foo-[subst {${build.cmd}}].tar.gz", true},
		{"a condition", "if {[string match {*g*} ${build.cmd}]} {distfiles a.tgz}", true},
		{"expr", "set m [expr {${build.cmd} eq \"\" ? \"a\" : \"b\"}]\ndistfiles $m.tgz", true},
		{"switch", "switch -glob ${build.cmd} { *g* {distfiles a} default {distfiles b}}", true},
		{"a proc's return", "proc f {} {global build.cmd; return ${build.cmd}}\ndistfiles [f].tgz", true},
		{"a proc alone", "proc f {} {global build.cmd; return ${build.cmd}}", true},
		{"a tainted variable", "set m ${build.cmd}\nif {$m eq \"\"} {distfiles a}", true},
		{"a tainted variable by [set]", "set m ${build.cmd}\ndistfiles [set m].tgz", true},
		{"a multi-line condition", "if {[info exists x] ||\n ${build.cmd} eq {}} {distfiles a}", true},
		{"through test.cmd", "test.cmd ${build.cmd}\ndistfiles foo-[file tail ${test.cmd}].tgz", true},
		{"through test.cmd by [option]", "test.cmd ${build.cmd}\ndistfiles foo-[option test.cmd].tgz", true},
		{"through test.cmd and a variable", "set m ${build.cmd}\ntest.cmd $m\nmaster_sites http://x/${test.cmd}", true},
		{"through build.env", "build.env MAKE=${build.cmd}\nmaster_sites http://x/${build.env}", true},
		{"through two options", "build.env MAKE=${build.cmd}\ntest.env ${build.env}\ndistfiles ${test.env}.tgz", true},
		{"catch", "if {[catch {set m ${build.cmd}}]} {distfiles a}", true},
		{"string length", "set m [string length ${build.cmd}]\ndistfiles $m.tgz", true},
		{"a multi-line expr", "set m [expr {\n${build.cmd} eq {} ? 1 : 2}]\ndistfiles $m.tgz", true},
		{"lassign", "lassign ${build.cmd} m\ndistfiles $m.tgz", true},
		{"append", "append m ${build.cmd}\ndistfiles $m.tgz", true},
		{"git.branch", "git.branch ${build.cmd}", true},
		{"checksums", "checksums sha256 ${build.cmd}", true},
		{"a default of its own", "default test.cmd {${build.cmd}}\ndistfiles ${test.cmd}", true},
	} {
		require.Equal(t, c.host, b.tolerate(t, c.contents, getmake), "%s", c.name)
	}
}

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
	require.False(t, b.tolerate(t, "build.args ${configure.sdkroot}", xcrun(b.file("port1.0/portconfigure.tcl"))), "Base's own xcrun for the SDK")
	require.True(t, b.tolerate(t, "build.args ${configure.sdkroot}", xcrun(thePortfile)), "a Portfile's xcrun under a portconfigure name")
}
