package observe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// A watched procedure is followed, within architecture's bounds on #110's
// finding 1 (A1 to A4, and B): its body is judged, each mention of a
// watched value in it a benign sink's argument or what it returns; its
// calls are placed only as a substitution in a benign sink's arguments, in
// a default's text, or, where its body touches benign sinks alone, as a
// statement; a procedure calling a watched one is watched; a default whose
// text a watched value reaches watches its option, which fails the context
// where it's a field the model acts on. A whole literal word naming a
// watched option in a benign sink's arguments is text, not a read (B).
// Anything else stays inconclusive.
func TestAWatchedProcedureIsFollowedWithinItsBounds(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	getmake := b.lookup("make", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	// The Portfile's own [option build.cmd] stays inconclusive by the
	// older expression rule, so the procedure reads the global.
	returns := "proc p {} {\n    global build.cmd\n    if {![exists build.cmd]} {\n        return \"\"\n    }\n    set m ${build.cmd}\n    return \"MAKE=$m\"\n}\n"
	for _, c := range []struct {
		name, contents string
		host           bool
	}{
		{"its call in a build option", returns + "build.args [p]", false},
		{"its call in a default's text", returns + "default test.args {[p] check}", false},
		{"a statement call of one that sets build options alone", "proc p {} {\n    global build.cmd\n    build.args MAKE=${build.cmd}\n}\np", false},
		{"one calling a watched one", returns + "proc q {} {\n    return [p]\n}\nbuild.args [q]", false},
		{"a literal word naming it in a build option", "build.args SHELL=/bin/sh build.cmd", false},
		{"a body writing distfiles, its calls placed", "proc p {} {\n    global build.cmd\n    distfiles x-${build.cmd}.tgz\n    return x\n}\nbuild.args [p]", true},
		{"a body writing distfiles as its last command", "proc p {} {\n    global build.cmd\n    distfiles x-${build.cmd}.tgz\n}\nbuild.args [p]", true},
		{"its call in distfiles", returns + "distfiles x-[p].tgz", true},
		{"its call in master_sites", returns + "master_sites http://x/[p]/", true},
		{"a default of distfiles", returns + "default distfiles {[p].tgz}", true},
		{"a two-step default chain to distfiles", returns + "default test.args {[p]}\ndefault distfiles {${test.args}.tgz}", true},
		{"a default chain to distfiles by a read", returns + "default test.args {[p]}\ndistfiles ${test.args}.tgz", true},
		{"called through eval", returns + "build.args [eval p]", true},
		{"called through a variable", returns + "set cmd p\nbuild.args [$cmd]", true},
		{"registered as a callback", returns + "port::register_callback p", true},
		{"renamed", returns + "rename p q\nbuild.args [q]", true},
		{"a statement call of one that returns", returns + "p", true},
		{"calling one whose value reaches distfiles", returns + "proc q {} {\n    distfiles [p].tgz\n    return x\n}\nbuild.args [q]", true},
		{"one calling a watched one, called in distfiles", returns + "proc q {} {\n    return [p]\n}\ndistfiles [q].tgz", true},
		{"a local set flowing to a non-benign command", "proc p {} {\n    global build.cmd\n    set m ${build.cmd}\n    distfiles $m.tgz\n    return x\n}\nbuild.args [p]", true},
		{"a literal word naming a watched procedure", returns + "build.args p", true},
		{"a literal word naming it in distfiles", "distfiles build.cmd", true},
		{"a bracketed word naming it", "build.args [list build.cmd]", true},
		// Architecture's conditions on accepting A and B.
		{"a port's own option, read by distfiles", returns + "options ksharch\ndefault ksharch {[p]}\ndistfiles x-${ksharch}.tgz", true},
		{"a port's own option, read in phase bodies", returns + "options ksharch\ndefault ksharch {[p]}\npost-build {\n    system \"ls arch/${ksharch}\"\n}\ndestroot {\n    xinstall -m 0755 arch/${ksharch}/ksh ${destroot}${prefix}/bin\n}", false},
		{"a two-step chain of the port's own options to master_sites", returns + "default a {[p]}\ndefault b {${a}}\nmaster_sites ${b}", true},
		{"a watched option given a handler", returns + "options ksharch\ndefault ksharch {[p]}\noption_proc ksharch handler\nproc handler {option action args} {}", true},
		{"a watched option traced", returns + "options ksharch\ndefault ksharch {[p]}\ntrace add variable ksharch write handler\nproc handler {args} {}", true},
		{"a PortGroup-style option whose handler writes master_sites", returns + "options gh.version\noption_proc gh.version gh.handler\nproc gh.handler {option action args} {\n    if {$action eq \"set\"} {\n        master_sites https://x/$args/\n    }\n}\ndefault gh.version {[p]}", true},
		{"an uncalled procedure writing distfiles", "proc f {} {\n    global build.cmd\n    distfiles x-${build.cmd}.tgz\n    return x\n}", true},
		{"mentions after a global, judged", "proc f {} {\n    global build.cmd\n    git.branch ${build.cmd}\n    return x\n}", true},
		{"a dynamic call into a set", "proc p_y {} {\n    global build.cmd\n    return ${build.cmd}\n}\nset x p\nset v [${x}_y]", true},
		{"eval of built text", "proc p_y {} {\n    global build.cmd\n    return ${build.cmd}\n}\nset s [string cat p _y]\neval $s", true},
		{"a dynamic call in a condition", "proc p_y {} {\n    global build.cmd\n    return ${build.cmd}\n}\nset x p\nif {[${x}_y] eq \"\"} {distfiles a}", true},
		{"a dynamic call in a nested expression", "proc p_y {} {\n    global build.cmd\n    return ${build.cmd}\n}\nset x p\nset v [expr {[${x}_y] eq {}}]", true},
		{"a dynamic call in a build option", "proc p_y {} {\n    global build.cmd\n    return ${build.cmd}\n}\nset x p\nbuild.args [${x}_y]", false},
		{"a dynamic call in a phase body", "proc p_y {} {\n    global build.cmd\n    return ${build.cmd}\n}\nset x p\npost-build {\n    set v [${x}_y]\n}", false},
		{"a dynamic call with no watched procedure", "set x p\nset v [${x}_y]", false},
	} {
		require.Equal(t, c.host, b.tolerate(t, c.contents, getmake), "%s", c.name)
	}
}

// The three shapes the survey of #110's round moved, each Base's own read
// whose value reaches build positions alone: the haskell_cabal PortGroup's
// build_getjobsarg turning build.jobs into -jN for build.post_args, called
// in its defaults and by ghc in its own build.post_args; ksh93's ksharch
// procedure reading build.env, which carries build.jobs, for a default
// read only in phase bodies; and wireshark's continued line, whose
// configure.args-append is a literal word of configure.args-replace.
func TestTheSurveysThreeShapesAreExplained(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	root := t.TempDir()
	group := filepath.Join(root, "_resources", "port1.0", "group", "hcabal-1.0.tcl")
	require.NoError(t, os.MkdirAll(filepath.Dir(group), 0o755))
	require.NoError(t, os.WriteFile(group, []byte(`proc hcabal.build_getjobsarg {args} {
    global build.jobs use_parallel_build
    if {![exists build.jobs] || ![tbool use_parallel_build]} {
        return ""
    }

    set jobs [option build.jobs]
    if {![string is integer -strict $jobs] || $jobs < 1} {
        return ""
    }
    return "-j$jobs"
}

default build.post_args     {\
                                [hcabal.build_getjobsarg]\
                                --prefix=${prefix}\
                            }
default destroot.post_args  {\
                                [hcabal.build_getjobsarg]\
                                --installdir=${destroot}${prefix}/bin\
                            }
`), 0o644))
	jobs := func(at ...macports.SourceFrame) macports.Declaration {
		frames := append(at, b.substitution("build.jobs", "[portbuild::build_getjobs]")...)
		frames = append(frames, frame("portbuild::build_getjobs"),
			macports.SourceFrame{File: b.file("port1.0/portbuild.tcl"), Line: 152, Command: "macports_try -pass_signal {\n    set jobs [sysctl hw.activecpu]\n}"},
			macports.SourceFrame{File: b.file("macports1.0/macports_util.tcl"), Line: 367, Command: "uplevel 1 [list ::builtin_catch $trybody $EMVAR $OPTVAR]"},
			macports.SourceFrame{Line: 2, Command: "sysctl hw.activecpu"})
		return macports.Declaration{Command: "dockhand.host-access", Values: []string{"modeled context depends on this Mac's kernel, read with sysctl, while evaluating the Portfile", "closed", "portfile", "sysctl hw.activecpu"}, Frames: frames}
	}
	tolerate := func(contents string, d macports.Declaration) bool {
		port := macports.PortObservation{ModeledHostAccess: true, Problems: []string{d.Values[0]}, Declarations: []macports.Declaration{d}, BaseLibrary: b.lib}
		_, inconclusive := Tolerate(t.Context(), port, []byte(contents), root)
		return inconclusive
	}
	portfile := filepath.Join(root, "devel", "alex", "Portfile")
	alex := "PortSystem 1.0\nPortGroup hcabal 1.0\nname alex\nversion 1\nbuild.post_args-append --bindir=${prefix}/bin\n"
	viaPostArgs := jobs(append([]macports.SourceFrame{{File: portfile, Line: 5, Command: "build.post_args-append --bindir=${prefix}/bin"}}, b.substitution("build.post_args", " [hcabal.build_getjobsarg] --prefix=${prefix} ")...)...)
	viaPostArgs.Frames = append(viaPostArgs.Frames[:4:4], append([]macports.SourceFrame{frame("hcabal.build_getjobsarg"), {File: group, Line: 7, Command: "option build.jobs"}}, viaPostArgs.Frames[4:]...)...)
	require.False(t, tolerate(alex, viaPostArgs), "alex: build.jobs through the PortGroup's build.post_args default")

	ghc := "PortSystem 1.0\nPortGroup hcabal 1.0\nname ghc\nversion 1\nbuild.post_args -VVVVV [hcabal.build_getjobsarg]\n"
	direct := jobs(macports.SourceFrame{File: portfile, Line: 5, Command: "hcabal.build_getjobsarg"}, macports.SourceFrame{File: group, Line: 7, Command: "option build.jobs"})
	require.False(t, tolerate(ghc, direct), "ghc: the PortGroup's procedure called in its own build.post_args")
	require.True(t, tolerate("PortSystem 1.0\nPortGroup hcabal 1.0\nname ghc\nversion 1\ndistfiles ghc-[hcabal.build_getjobsarg].tgz\n", direct), "the procedure called in distfiles")

	ksh := `PortSystem 1.0
name ksh93
version 1
build.cmd           bin/package
build.env           "CC=${configure.cc}" \
                    "NPROC=${build.jobs}"
post-build {
    if {[catch {system -W ${build.dir} "fgrep '*** exit code' arch/${ksharch}/make.out && exit 1 || exit 0"}]} {
        return -code error "build failed"
    }
}
destroot {
    xinstall -m 0755 ${build.dir}/arch/${ksharch}/bin/ksh ${destroot}${prefix}/bin
}
options ksharch
default ksharch {[ksharch]}

proc ksharch {} {
    global build.cmd build.dir build.env
    return [exec env {*}${build.env} ${build.dir}/${build.cmd} host type]
}
`
	own := jobs()
	own.Values[2] = "base"
	require.False(t, tolerate(ksh, own), "ksh93: build.env carries build.jobs into ksharch, read in phase bodies")
	require.True(t, tolerate(ksh+"distfiles ksh-${ksharch}.tgz\n", own), "ksharch read by distfiles")
	require.True(t, tolerate(ksh+"version [ksharch]\n", own), "the procedure called in version")

	getmake := b.lookup("make", "base", b.substitution("configure.args", "[list -DCMAKE_BUILD_TYPE=Release]"), b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	wireshark := "PortSystem 1.0\nname wireshark\nversion 1\nconfigure.args-append -DBUILD_wireshark=OFF\nvariant kerberos {\n    configure.args-replace  -DENABLE_KERBEROS=OFF -DENABLE_KERBEROS=ON \\\n    configure.args-append   -DCMAKE_SHARED_LINKER_FLAGS=\"-lk5crypto\"\n}\n"
	require.False(t, tolerate(wireshark, getmake), "wireshark: a continued line's literal word")
}

// A variant whose body is a quoted string, as wireshark3's python variants
// are, made in a loop, runs that string as its script: a watched option it
// sets there is a write, and a watched value it reads is judged where the
// script puts it.
func TestAQuotedVariantBodyIsWalkedAsAScript(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	getmake := b.lookup("make", "base", b.substitution("configure.args", "[list -DCMAKE_BUILD_TYPE=Release]"), b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	writes := "foreach v {39 310} {\n    variant py$v description \"Use python $v\" \"\n        depends_build-append port:py$v\n        configure.args-append -DPython3_ROOT_DIR=\\\"/x/$v/bin\\\"\n    \"\n}"
	require.False(t, b.tolerate(t, writes, getmake), "a quoted body setting configure.args")
	reads := "variant x description {x} \"\n    distfiles x-[file tail ${build.cmd}].tgz\n\""
	require.True(t, b.tolerate(t, reads, getmake), "a quoted body putting build.cmd in distfiles")
	// Substituted as the Portfile is read, whatever the selection: a
	// watched value in a command word names the command.
	command := "variant x description {x} \"\n    ${build.cmd} foo\n\""
	require.True(t, b.tolerate(t, command, getmake), "a quoted body whose command word is build.cmd's value")
}

// A command inside a command substitution is judged for what it does, by
// the rules of a command at the top, before the outer command's sink rule:
// a nested write to a fetch field fails, a nested set carries the value to
// its variable, a nested option assignment watches its option, inside a
// procedure's body and its return as at the top (the outside confirmation
// of #110 at 6ab59afb, finding 1). A nested command that only computes a
// value hands it to the outer command, which judges it.
func TestANestedCommandIsJudgedForWhatItDoes(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	getmake := b.lookup("make", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	for _, c := range []struct {
		name, contents string
		host           bool
	}{
		{"a nested distfiles", "build.args [distfiles x-${build.cmd}.tgz]", true},
		{"a nested distfiles in a quoted word", "build.args \"-D[distfiles x-${build.cmd}.tgz]\"", true},
		{"a nested set read by distfiles", "build.args [set m ${build.cmd}]\ndistfiles x-$m.tgz", true},
		{"a nested append read by distfiles", "build.args [append m ${build.cmd}]\ndistfiles x-$m.tgz", true},
		{"a nested option read by distfiles", "build.args [test.cmd ${build.cmd}]\ndistfiles x-${test.cmd}.tgz", true},
		{"a nested distfiles in a procedure's body", "proc p {} {\n    global build.cmd\n    build.args [distfiles x-${build.cmd}.tgz]\n}\nbuild.args [p]", true},
		{"a nested distfiles in a return", "proc p {} {\n    global build.cmd\n    return [distfiles x-${build.cmd}.tgz]\n}\nbuild.args [p]", true},
		{"a nested distfiles in a default's text", "default test.args {[distfiles x-${build.cmd}.tgz]}", true},
		{"a nested distfiles in a caught script", "build.args [catch {distfiles x-${build.cmd}.tgz}]", true},
		{"a nested PortGroup procedure taking it", "build.args [github.setup a b ${build.cmd}]", true},
		{"a namespace variable is no global", "proc p {} {\n    variable build.cmd\n    distfiles x-${build.cmd}.tgz\n}\nbuild.args [p]", true},
		{"a nested value, handed to a build option", "build.args [file tail ${build.cmd}] [string toupper ${build.cmd}]", false},
		{"a nested set read by the build", "build.args [set m ${build.cmd}]\nbuild.target $m", false},
		{"a set read by the build", "set m ${build.cmd}\nbuild.target $m", false},
		{"a nested option read by the build", "build.args [test.cmd ${build.cmd}]\nbuild.target ${test.cmd}", false},
	} {
		require.Equal(t, c.host, b.tolerate(t, c.contents, getmake), "%s", c.name)
	}
}

// A handler, trace or callback registration whose subject isn't a literal
// can't be shown not to name a watched option or procedure: it fails the
// context (the outside confirmation of #110 at 6ab59afb, finding 2). A
// braced subject is a literal, judged by its text.
func TestAComputedRegistrationIsInconclusive(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	getmake := b.lookup("make", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	handler := "proc handler {option action args} {\n    if {$action eq {set}} {distfiles x-$args.tgz}\n}\n"
	for _, c := range []struct {
		name, contents string
		host           bool
	}{
		{"a computed option name", "set n [string cat test .cmd]\noption_proc $n handler\n" + handler + "test.cmd ${build.cmd}", true},
		{"a computed traced variable", "set n [string cat test .cmd]\ntrace add variable $n write handler\n" + handler + "test.cmd ${build.cmd}", true},
		{"a braced watched option", "option_proc {test.cmd} handler\n" + handler + "test.cmd ${build.cmd}", true},
		{"a literal unwatched option", "option_proc test.env handler\n" + handler + "test.cmd ${build.cmd}", false},
	} {
		require.Equal(t, c.host, b.tolerate(t, c.contents, getmake), "%s", c.name)
	}
}
