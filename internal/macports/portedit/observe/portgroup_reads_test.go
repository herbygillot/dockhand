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
