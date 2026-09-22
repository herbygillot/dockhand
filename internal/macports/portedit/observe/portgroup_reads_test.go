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
