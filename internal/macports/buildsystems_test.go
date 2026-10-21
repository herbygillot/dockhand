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

// A port's build reads its build files where its build systems name, below
// worksrcpath: i2pd's cmake.source_dir, build/. One at worksrcpath, or
// outside it, names none, and an option of a build system the port
// doesn't use is no build's (field testing at c643559a).
func TestTheDirectoriesAPortsBuildReads(t *testing.T) {
	t.Parallel()
	port := func(groups string, options map[string]string) PortInfo {
		options["dockhand.portgroups"] = groups
		options["use_configure"] = "no"
		options["worksrcdir"] = "i2pd-2.61.0"
		return PortInfo{Options: options}
	}
	i2pd := port("github cmake", map[string]string{"cmake.source_dir": "@worksrc@/build", "build.dir": "<build>/work/build", "configure.dir": "@worksrc@"})
	require.Equal(t, []string{"build"}, i2pd.BuildDirectories(), "build.dir, outside, and configure.dir, at the top, name none")
	require.Equal(t, project.Spec{Subdirectory: "", Directories: []string{"build"}}, i2pd.ReadingSpec())
	mixed := port("python cargo", map[string]string{"build.dir": "@worksrc@/bindings/python", "cargo.dir": "@worksrc@/rust/", "cmake.source_dir": "@worksrc@/unused"})
	require.Equal(t, []string{"bindings/python", "rust"}, mixed.BuildDirectories())
	require.Empty(t, port("github", map[string]string{"cmake.source_dir": "@worksrc@/build"}).BuildDirectories())
}
