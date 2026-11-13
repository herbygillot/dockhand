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
