package observe

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// java's callback finding JAVA_HOME on the host shapes the build alone: a
// closed read under it is explained where the Portfile reads java.home
// only in build positions, and not where a source reads it, where a
// Portfile frame is on the stack, or for a version of the PortGroup not
// read (#110).
func TestJavasCallbackIsABuildOnlyProbe(t *testing.T) {
	t.Parallel()
	problem := "modeled context depends on filesystem state outside the captured ports tree"
	read := func(group string, portfile bool) macports.Declaration {
		frames := []macports.SourceFrame{
			{Command: "port::run_callbacks"},
			{Line: 5, Command: "${callback}"},
			{File: "/w/_resources/port1.0/group/" + group + ".tcl", Line: 157, Command: "find_java_home"},
			{File: "/w/_resources/port1.0/group/" + group + ".tcl", Line: 108, Command: "file isdirectory $home_value"},
		}
		if portfile {
			frames = append([]macports.SourceFrame{{File: "/w/java/fixture/Portfile", Line: 9, Command: "java::java_set_env"}}, frames...)
		}
		return macports.Declaration{Command: "dockhand.host-access", Values: []string{problem, "closed", "portgroup", "file isdirectory /x"}, Frames: frames}
	}
	observed := func(d macports.Declaration) macports.PortObservation {
		return macports.PortObservation{ModeledHostAccess: true, Declarations: []macports.Declaration{d}, Problems: []string{problem}}
	}
	build := []byte("PortSystem 1.0\nPortGroup java 1.0\nname fixture\nversion 1\nconfigure.args --with-java=${java.home}\n")
	source := []byte("PortSystem 1.0\nPortGroup java 1.0\nname fixture\nversion 1\ndistfiles fixture-[file tail ${java.home}].tar.gz\n")

	_, inconclusive := Tolerate(context.Background(), observed(read("java-1.0", false)), build, "")
	require.False(t, inconclusive, "the callback's read, java.home read for the build")
	_, inconclusive = Tolerate(context.Background(), observed(read("java-1.0", false)), source, "")
	require.True(t, inconclusive, "java.home reaching a source")
	_, inconclusive = Tolerate(context.Background(), observed(read("java-1.0", true)), build, "")
	require.True(t, inconclusive, "a Portfile calling into java:: is judged as its own read")
	_, inconclusive = Tolerate(context.Background(), observed(read("java-2.0", false)), build, "")
	require.True(t, inconclusive, "a version of the PortGroup not read")
}
