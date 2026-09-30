package macports

import (
	"testing"

	"github.com/stretchr/testify/require"
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
		systems []BuildSystem
		known   bool
	}{
		{port("github cmake legacysupport", "yes", "/opt/local/bin/cmake"), []BuildSystem{CMake}, true},
		{port("golang", "no", "./configure"), []BuildSystem{Go}, true},
		{port("github", "yes", "./configure"), []BuildSystem{Autotools}, true},
		{port("python cargo", "no", "./configure"), []BuildSystem{Python, Cargo}, true},
		{port("github", "no", "./configure"), nil, false},
		{port("-", "yes", "./configure"), nil, false},
	} {
		systems, known := test.port.BuildSystems()
		require.Equal(t, test.systems, systems)
		require.Equal(t, test.known, known)
	}
}
