package observe

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

// A host read made inside a PortGroup file, in the condition of a branch
// whose bodies only set build-only options, is explained: the qt4
// PortGroup choosing a dependency path by whether the framework is
// installed. A read whose branch declares a source, or one made in a
// Portfile, is not.
func TestPortGroupReadsThatOnlyShapeTheBuildAreExplained(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	groups := filepath.Join(root, "_resources", "port1.0", "group")
	require.NoError(t, os.MkdirAll(groups, 0o755))
	group := filepath.Join(groups, "qt4-1.0.tcl")
	require.NoError(t, os.WriteFile(group, []byte(`# qt4
if {![info exists building_qt4]} {
    if {${os.platform} eq "darwin"} {
        if {[file exists ${qt_frameworks_dir}/QtCore/QtCore]} {
            depends_lib-append path:Library/Frameworks/QtCore/QtCore:qt4-mac
        } else {
            depends_lib-append path:lib/libQtCore.4.dylib:qt4-mac
        }
    }
}
if {[file exists ${prefix}/etc/mirror]} {
    master_sites ${prefix}/etc/mirror
}
set layout [file exists ${prefix}/lib/qt4]
`), 0o644))
	portfile := filepath.Join(root, "science", "fixture", "Portfile")
	access := func(file string, line int) macports.Declaration {
		return macports.Declaration{Command: "dockhand.host-access", Values: []string{"modeled context depends on filesystem state outside the captured ports tree"},
			Frames: []macports.SourceFrame{{File: portfile, Line: 5, Command: "PortGroup qt4 1.0"}, {File: file, Line: line, Command: "file exists ..."}, {File: "", Line: 1, Command: "::dockhand_observation::record"}}}
	}
	observed := func(declarations ...macports.Declaration) macports.PortObservation {
		return macports.PortObservation{HostAccess: true, Declarations: declarations, Problems: []string{"modeled context depends on filesystem state outside the captured ports tree"}}
	}
	contents := []byte("PortSystem 1.0\nPortGroup qt4 1.0\nname fixture\nversion 1\n")

	port, inconclusive := Tolerate(context.Background(), observed(access(group, 4)), contents, root)
	require.False(t, inconclusive, "a dependency choice inside the PortGroup is explained")
	require.Empty(t, port.Problems)
	require.False(t, port.HostAccess)

	_, inconclusive = Tolerate(context.Background(), observed(access(group, 11)), contents, root)
	require.True(t, inconclusive, "a branch that declares master sites is not")
	_, inconclusive = Tolerate(context.Background(), observed(access(group, 14)), contents, root)
	require.True(t, inconclusive, "a read stored in a variable is judged where it is used, which this does not follow")
	_, inconclusive = Tolerate(context.Background(), observed(access(portfile, 2)), contents, root)
	require.True(t, inconclusive, "a read in the Portfile itself is the Portfile's own")
	_, inconclusive = Tolerate(context.Background(), observed(access(group, 4), access(portfile, 2)), contents, root)
	require.True(t, inconclusive, "every access must be explained")
	_, inconclusive = Tolerate(context.Background(), observed(), contents, root)
	require.True(t, inconclusive, "host access without a recorded declaration stays inconclusive")
}

// qmake5 1.0's platform block reads use_xcode, whose default Base computes
// from the host, and asks whether the SDK is there to set it: both shape
// the build alone, so a port's plain version line and archives stay
// conclusive in a modeled context (librecad and cpi, the roadmap's v0.5.0
// part 3, item 2). A platform block that sets a source, or a Portfile whose
// own version reads use_xcode, or the host, stays inconclusive.
func TestHostStateTheVersionAndArchivesDontReadIsExplained(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	groups := filepath.Join(root, "_resources", "port1.0", "group")
	require.NoError(t, os.MkdirAll(groups, 0o755))
	group := filepath.Join(groups, "qmake5-1.0.tcl")
	require.NoError(t, os.WriteFile(group, []byte(`# qmake5
platform macosx {
    if { !${use_xcode} && ![file exists ${configure.developer_dir}/SDKs] } {
        use_xcode yes
    }
}
platform darwin {
    if {[file exists /opt/mirror]} {
        master_sites file:///opt/mirror
    }
}
`), 0o644))
	portfile := filepath.Join(root, "cad", "librecad", "Portfile")
	frame := func(command string) macports.SourceFrame {
		return macports.SourceFrame{File: "", Line: 1, Command: command}
	}
	access := func(frames ...macports.SourceFrame) macports.Declaration {
		return macports.Declaration{Command: "dockhand.host-access", Values: []string{"modeled context depends on filesystem state outside the captured ports tree"},
			Frames: append([]macports.SourceFrame{{File: portfile, Line: 5, Command: "PortGroup qmake5 1.0"}}, frames...)}
	}
	observed := func(declarations ...macports.Declaration) macports.PortObservation {
		return macports.PortObservation{HostAccess: true, Declarations: declarations, Problems: []string{"modeled context depends on filesystem state outside the captured ports tree"}}
	}
	plain := []byte("PortSystem 1.0\nPortGroup qmake5 1.0\nname librecad\ngithub.setup LibreCAD LibreCAD 2.2.1.5 v\n")
	byDefault := access(macports.SourceFrame{File: group, Line: 2, Command: "platform macosx {"}, frame("default_check use_xcode {} read"), frame("file exists /usr/lib/libxcselect.dylib"))
	sdk := access(macports.SourceFrame{File: group, Line: 2, Command: "platform macosx {"}, frame("file exists ${configure.developer_dir}/SDKs"))

	port, inconclusive := Tolerate(context.Background(), observed(byDefault, sdk), plain, root)
	require.False(t, inconclusive, "use_xcode's default and the SDK's check shape the build alone")
	require.Empty(t, port.Problems)

	_, inconclusive = Tolerate(context.Background(), observed(access(macports.SourceFrame{File: group, Line: 7, Command: "platform darwin {"}, frame("file exists /opt/mirror"))), plain, root)
	require.True(t, inconclusive, "a platform block whose branch sets master sites reaches the archives")

	readsIt := []byte("PortSystem 1.0\nPortGroup qmake5 1.0\nname librecad\nversion [expr {${use_xcode} ? 2 : 1}]\n")
	versionRead := access(macports.SourceFrame{File: portfile, Line: 4, Command: "version [expr {${use_xcode} ? 2 : 1}]"}, frame("default_check use_xcode {} read"), frame("file exists /usr/lib/libxcselect.dylib"))
	_, inconclusive = Tolerate(context.Background(), observed(byDefault, versionRead), readsIt, root)
	require.True(t, inconclusive, "a version that reads use_xcode reads the host through its default")

	_, inconclusive = Tolerate(context.Background(), observed(access(macports.SourceFrame{File: portfile, Line: 4, Command: "version [file exists /opt/x]"})), plain, root)
	require.True(t, inconclusive, "a version line that reads the host itself")
}

// A read the closed model answered is weighed as a host read is, whoever
// asked: Base's compiler probe, through portconfigure, is explained where
// the Portfile reads toolchain options only in build positions; one Base
// makes elsewhere, which may reach a fetch field, leaves the context
// inconclusive; both keep the ledger that marked them closed (#110).
func TestAClosedReadIsWeighedAsAHostRead(t *testing.T) {
	t.Parallel()
	problem := "modeled context depends on filesystem state outside the captured ports tree"
	library := "/opt/local/libexec/macports/lib"
	closed := func(command string) macports.Declaration {
		return macports.Declaration{Command: "dockhand.host-access", Values: []string{problem, "closed", "base", "file exists /usr/include/sys/cdefs.h"},
			Frames: []macports.SourceFrame{{Line: 1, Command: command}, {File: library + "/port1.0/portconfigure.tcl", Line: 1, Command: "file exists /usr/include/sys/cdefs.h"}}}
	}
	ledger := []macports.LedgerEntry{{Command: "file", Subcommand: "exists", Source: "closed", Count: 1}}
	observed := func(declaration macports.Declaration) macports.PortObservation {
		return macports.PortObservation{ModeledHostAccess: true, Declarations: []macports.Declaration{declaration}, Problems: []string{problem}, Ledger: ledger, BaseLibrary: library}
	}
	contents := []byte("PortSystem 1.0\nname fixture\nversion 1\nconfigure.args CC=${configure.cc}\n")
	port, inconclusive := Tolerate(context.Background(), observed(closed("portconfigure::configure_get_compiler cc")), contents, "")
	require.False(t, inconclusive, "Base's compiler probe, with the toolchain read in build positions")
	require.Equal(t, ledger, port.Ledger)
	port, inconclusive = Tolerate(context.Background(), observed(closed("portfetch::checkfiles")), contents, "")
	require.True(t, inconclusive, "a closed read Base makes outside the compiler probe")
	require.Equal(t, ledger, port.Ledger)
}

// Base's build.jobs default asks the kernel for its CPU count and memory
// (build_getjobs, portbuild.tcl:152), which a modelled context closes; a
// build-only option, so the read is explained where neither the Portfile
// nor a PortGroup on the stack reads build.jobs outside a build position,
// as a host-in-model read is. The Portfile's own CPU-feature read stays
// inconclusive, and so does one whose version reads build.jobs (#110).
func TestBuildJobsDefaultIsExplainedWhereOnlyTheBuildReadsIt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	portfile := filepath.Join(root, "devel", "cmake", "Portfile")
	sysctl := "modeled context depends on this Mac's kernel, read with sysctl, while evaluating the Portfile"
	access := func(at macports.SourceFrame, frames ...macports.SourceFrame) macports.Declaration {
		return macports.Declaration{Command: "dockhand.host-access", Values: []string{sysctl, "closed", "base"}, Frames: append([]macports.SourceFrame{at}, frames...)}
	}
	base := newBaseFixture(t)
	observed := func(declarations ...macports.Declaration) macports.PortObservation {
		return macports.PortObservation{ModeledHostAccess: true, Declarations: declarations, Problems: []string{sysctl}, BaseLibrary: base.lib}
	}
	jobs := func(at macports.SourceFrame) macports.Declaration {
		return access(at, frame("default_check build.jobs {} read"), frame("subst {[portbuild::build_getjobs]}"), frame("portbuild::build_getjobs"),
			macports.SourceFrame{File: base.file("port1.0/portbuild.tcl"), Line: 152, Command: "macports_try -pass_signal {\n    set jobs [sysctl hw.activecpu]\n}"})
	}
	building := []byte("PortSystem 1.0\nname cmake\nversion 3.31.12\nbuild.args -j${build.jobs}\n")
	port, inconclusive := Tolerate(context.Background(), observed(jobs(macports.SourceFrame{File: portfile, Line: 4, Command: "build.args -j${build.jobs}"})), building, root)
	require.False(t, inconclusive, "build.jobs shapes the build alone")
	require.Empty(t, port.Problems)

	versioned := []byte("PortSystem 1.0\nname cmake\nversion [expr {${build.jobs} > 4 ? 2 : 1}]\n")
	_, inconclusive = Tolerate(context.Background(), observed(jobs(macports.SourceFrame{File: portfile, Line: 3, Command: "version [expr {${build.jobs} > 4 ? 2 : 1}]"})), versioned, root)
	require.True(t, inconclusive, "a version that reads build.jobs reads the kernel through its default")

	feature := access(macports.SourceFrame{File: portfile, Line: 5, Command: "if {[sysctl hw.optional.avx2_0]} {"}, frame("sysctl hw.optional.avx2_0"))
	_, inconclusive = Tolerate(context.Background(), observed(feature), building, root)
	require.True(t, inconclusive, "a Portfile's own CPU-feature read stays closed and weighed")

	other := access(macports.SourceFrame{File: portfile, Line: 4, Command: "build.args -j${build.jobs}"}, frame("default_check build.jobs {} read"), frame("file exists /etc/jobs"))
	other.Values[0] = "modeled context depends on filesystem state outside the captured ports tree"
	_, inconclusive = Tolerate(context.Background(), observed(other), building, root)
	require.True(t, inconclusive, "only build_getjobs's sysctl is explained by the default")

	// The master's closed answers before the plan have no declaration to
	// explain them by: with the build.jobs read explained beside them, the
	// context stays inconclusive, and isn't a refusal of the port.
	master := "modeled context has no answer for what MacPorts asked of this release's developer tools (xcode-select): the facts table doesn't have them"
	both := observed(jobs(macports.SourceFrame{File: portfile, Line: 4, Command: "build.args -j${build.jobs}"}))
	both.Problems = append(both.Problems, master)
	_, inconclusive = Tolerate(context.Background(), both, building, root)
	require.True(t, inconclusive, "a host problem nothing explains")
}
