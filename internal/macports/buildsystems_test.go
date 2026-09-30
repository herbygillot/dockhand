package macports

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/project"
)

func TestAPortsBuildSystemsAreItsPortGroupsAndItsConfigure(t *testing.T) {
	t.Parallel()
	port := func(groups, configure, cmd string) PortInfo {
		options := map[string]string{"use_configure": configure, "configure.cmd": cmd}
		if groups != "-" {
			options["dockhand.portgroups"] = groups
		}
		return PortInfo{Options: options}
	}
	for _, test := range []struct {
		port    PortInfo
		systems []project.System
		known   bool
	}{
		{port("github cmake legacysupport", "yes", "/opt/local/bin/cmake"), []project.System{project.CMake}, true},
		{port("golang", "no", "./configure"), []project.System{project.Go}, true},
		{port("github", "yes", "./configure"), []project.System{project.Autotools}, true},
		{port("python cargo", "no", "./configure"), []project.System{project.Python, project.Cargo}, true},
		{port("github", "no", "./configure"), nil, false},
		{port("-", "yes", "./configure"), nil, false},
	} {
		systems, known := test.port.BuildSystems()
		require.Equal(t, test.systems, systems)
		require.Equal(t, test.known, known)
	}
}

// A port builds in its worksrcdir, which names the directory a distfile
// extracts to and then any subdirectory below it; the Go PortGroup's
// GOPATH location names none, being made from the top after extraction.
func TestTheSubdirectoryAPortBuildsIn(t *testing.T) {
	t.Parallel()
	for worksrcdir, want := range map[string]string{
		"uni-2.10.0": "", "demo-1.0/bindings/python": "bindings/python", "/demo-1.0/python/": "python", "": "",
		"gopath/src/github.com/cli/cli/v2": "",
	} {
		require.Equal(t, want, SourceSubdirectory(worksrcdir), worksrcdir)
	}
	require.False(t, GOPATHLayout("uni-2.10.0"))
	require.True(t, GOPATHLayout("gopath/src/github.com/cli/cli/v2"))
}
