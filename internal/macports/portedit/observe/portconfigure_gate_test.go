package observe

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

const portconfigureHostProblem = "modeled context depends on filesystem state outside the captured ports tree"

// qt53's Portfile reads configure.compiler outside build positions, so its
// PortGroup-entered portconfigure read stays inconclusive under #247.
func TestPortconfigurePortGroupReadIsGatedByQt53ToolchainShape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	groupDir := filepath.Join(root, "_resources", "port1.0", "group")
	require.NoError(t, os.MkdirAll(groupDir, 0o755))
	group := filepath.Join(groupDir, "qt53-1.0.tcl")
	require.NoError(t, os.WriteFile(group, []byte("if {[file exists /opt/qt53]} {\n    configure.args-append -DQT53=1\n}\n"), 0o644))
	portfile := filepath.Join(root, "devel", "qt53", "Portfile")
	baseFile := filepath.Join(root, "libexec", "macports", "lib", "port1.0", "portconfigure.tcl")
	d := macports.Declaration{Command: "dockhand.host-access", Values: []string{portconfigureHostProblem}, Frames: []macports.SourceFrame{
		{File: portfile, Line: 4, Command: "PortGroup qt53 1.0"},
		{File: group, Line: 1, Command: "if {[file exists /opt/qt53]} {"},
		{File: baseFile, Line: 571, Command: "portconfigure::configure_get_sdkroot"},
		{File: baseFile, Line: 571, Command: "file exists /usr/include/sys/cdefs.h"},
	}}
	port := macports.PortObservation{ModeledHostAccess: true, Problems: []string{portconfigureHostProblem}, Declarations: []macports.Declaration{d}, BaseLibrary: filepath.Dir(filepath.Dir(baseFile))}
	contents := []byte("PortSystem 1.0\nPortGroup qt53 1.0\nname qt53\nversion 5.3\nif {${configure.compiler} eq \"clang\"} { distfiles alternate.tar.gz }\nconfigure.args ${configure.compiler}\n")
	_, inconclusive := Tolerate(context.Background(), port, contents, root)
	require.True(t, inconclusive, "qt53's PortGroup read stays inconclusive when configure.compiler is read outside build positions (#247)")
}

// A build-only PortGroup read remains explainable when the #247 toolchain gate passes.
func TestPortconfigurePortGroupBuildOnlyReadIsExplainedWhenToolchainIsBenign(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	groupDir := filepath.Join(root, "_resources", "port1.0", "group")
	require.NoError(t, os.MkdirAll(groupDir, 0o755))
	group := filepath.Join(groupDir, "qt53-1.0.tcl")
	require.NoError(t, os.WriteFile(group, []byte("if {[file exists /opt/qt53]} {\n    configure.args-append -DQT53=1\n}\n"), 0o644))
	portfile := filepath.Join(root, "devel", "qt53", "Portfile")
	baseFile := filepath.Join(root, "libexec", "macports", "lib", "port1.0", "portconfigure.tcl")
	d := macports.Declaration{Command: "dockhand.host-access", Values: []string{portconfigureHostProblem}, Frames: []macports.SourceFrame{
		{File: portfile, Line: 4, Command: "PortGroup qt53 1.0"},
		{File: group, Line: 1, Command: "if {[file exists /opt/qt53]} {"},
		{File: baseFile, Line: 571, Command: "portconfigure::configure_get_sdkroot"},
		{File: baseFile, Line: 571, Command: "file exists /usr/include/sys/cdefs.h"},
	}}
	port := macports.PortObservation{ModeledHostAccess: true, Problems: []string{portconfigureHostProblem}, Declarations: []macports.Declaration{d}, BaseLibrary: filepath.Dir(filepath.Dir(baseFile))}
	contents := []byte("PortSystem 1.0\nPortGroup qt53 1.0\nname qt53\nversion 5.3\nconfigure.args ${configure.compiler}\n")
	result, inconclusive := Tolerate(context.Background(), port, contents, root)
	require.False(t, inconclusive, "the benign toolchain gate allows the PortGroup's build-only explanation (#247)")
	require.Empty(t, result.Problems)
}

// A Portfile-entered read stays unexplained on a benign port under #247.
func TestPortfileEnteredPortconfigureReadIsInconclusiveOnBenignToolchainPort(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	portfile := filepath.Join(root, "devel", "fixture", "Portfile")
	baseFile := filepath.Join(root, "libexec", "macports", "lib", "port1.0", "portconfigure.tcl")
	d := macports.Declaration{Command: "dockhand.host-access", Values: []string{portconfigureHostProblem}, Frames: []macports.SourceFrame{
		{File: portfile, Line: 4, Command: "configure"},
		{File: baseFile, Line: 1200, Command: "universal_setup"},
		{File: baseFile, Line: 571, Command: "portconfigure::configure_get_sdkroot"},
		{File: baseFile, Line: 571, Command: "file exists /usr/include/sys/cdefs.h"},
	}}
	port := macports.PortObservation{ModeledHostAccess: true, Problems: []string{portconfigureHostProblem}, Declarations: []macports.Declaration{d}, BaseLibrary: filepath.Dir(filepath.Dir(baseFile))}
	contents := []byte("PortSystem 1.0\nname fixture\nversion 1\nconfigure.args ${configure.compiler}\n")
	_, inconclusive := Tolerate(context.Background(), port, contents, root)
	require.True(t, inconclusive, "a Portfile-entered read remains unexplained despite benign toolchain reads (#247)")
}

// A recognized Base build-default read takes precedence over the #247
// toolchainProbe rule, even when configure.compiler is read outside build positions.
func TestPortconfigureBuildDefaultReadPrecedesNonBenignToolchainProbe(t *testing.T) {
	t.Parallel()
	b := newBaseFixture(t)
	contents := []byte("PortSystem 1.0\nname fixture\nversion ${configure.compiler}\n")
	require.False(t, toolchainReadsBenign(contents), "configure.compiler is read outside a build position")
	frames := b.substitution("build.jobs", "[portbuild::build_getjobs]")
	frames = append(frames,
		frame("portbuild::build_getjobs"),
		macports.SourceFrame{File: b.file("port1.0/portbuild.tcl"), Line: 152, Command: "sysctl hw.activecpu"},
		macports.SourceFrame{File: b.file("port1.0/portconfigure.tcl"), Line: 571, Command: "portconfigure::configure_get_sdkroot"},
		macports.SourceFrame{File: b.file("port1.0/portconfigure.tcl"), Line: 571, Command: "sysctl hw.activecpu"},
	)
	d := macports.Declaration{Command: "dockhand.host-access", Values: []string{
		"modeled context depends on this Mac's kernel, read with sysctl, while evaluating the Portfile", "closed", "base",
	}, Frames: frames}
	port := macports.PortObservation{HostAccess: true, Problems: []string{d.Values[0]}, Declarations: []macports.Declaration{d}, BaseLibrary: b.lib}
	result, inconclusive := Tolerate(context.Background(), port, contents, "")
	require.False(t, inconclusive, "Base's build-default rule still explains the portconfigure read before toolchainProbe (#247)")
	require.Empty(t, result.Problems)
}
