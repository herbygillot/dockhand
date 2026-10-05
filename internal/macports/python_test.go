package macports

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPythonSubportNamesItsPackage(t *testing.T) {
	t.Parallel()
	for port, want := range map[string]string{"py313-textual-fastdatatable": "textual-fastdatatable", "py39-zope.interface": "zope.interface", "py-textual-fastdatatable": "", "python313": "", "pypy-foo": ""} {
		got, ok := PythonPackage(port)
		require.Equal(t, want != "", ok, port)
		require.Equal(t, want, got, port)
	}
}

// A Python port provides the project its python.rootname names, as
// py313-yaml provides PyYAML, or its name's where that wasn't read; and
// the project its forge setup names, as py313-protobuf3 provides
// protobuf.
func TestAPythonPortProvidesItsProjects(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"pyyaml"}, PortInfo{Name: "py313-yaml", Options: map[string]string{"python.rootname": "pyyaml"}}.PythonProjects())
	require.Equal(t, []string{"Pillow"}, PortInfo{Name: "py313-Pillow"}.PythonProjects())
	require.Equal(t, []string{"protobuf3", "protobuf"}, PortInfo{Name: "py313-protobuf3", Options: map[string]string{"python.rootname": "protobuf3", "github.project": "protobuf"}}.PythonProjects())
	require.Empty(t, PortInfo{Name: "jq", Options: map[string]string{"github.project": "jq"}}.PythonProjects())
}

// A Python port's name says the Python version it's built for.
func TestAPythonPortsVersion(t *testing.T) {
	for port, want := range map[string]string{"py313-requests": "3.13", "py27-six": "2.7", "py310-textual-fastdatatable": "3.10"} {
		got, ok := PythonVersion(port)
		require.True(t, ok, port)
		require.Equal(t, want, got, port)
	}
	for _, port := range []string{"py-requests", "requests", "python313"} {
		_, ok := PythonVersion(port)
		require.False(t, ok, port)
	}
}

// The Pythons a port builds for are a subport's own, else its
// python.versions, else the python.version an application's port pins;
// none for a port of another kind, or one whose python.versions couldn't
// be settled and that pins none.
func TestThePythonsAPortBuildsFor(t *testing.T) {
	for _, test := range []struct {
		port PortInfo
		want []string
	}{
		{PortInfo{Name: "py313-requests", Options: map[string]string{"python.versions": "310 311 312 313", "python.version": "313"}}, []string{"3.13"}},
		{PortInfo{Name: "py-requests", Options: map[string]string{"python.versions": "310 311 312 313", "python.version": "313"}}, []string{"3.10", "3.11", "3.12", "3.13"}},
		{PortInfo{Name: "sshuttle", Options: map[string]string{"python.version": "313", "python.default_version": "313"}}, []string{"3.13"}},
		{PortInfo{Name: "sshuttle", Options: map[string]string{"python.version": "313"}, OptionErrors: map[string]string{"python.versions": "boom"}}, []string{"3.13"}},
		{PortInfo{Name: "py27-six", Options: map[string]string{}}, []string{"2.7"}},
		{PortInfo{Name: "jq", Options: map[string]string{}}, nil},
	} {
		require.Equal(t, test.want, test.port.Pythons(), test.port.Name)
	}
	require.Equal(t, "python313", PythonPort("3.13"))
	require.Equal(t, "python27", PythonPort("2.7"))
}

// A Python subport new to a directory is compared with the base's newest
// subport of its package; any other port with itself.
func TestANewPythonSubportsCounterpartIsItsSibling(t *testing.T) {
	t.Parallel()
	base := []PortInfo{{Name: "py-coremltools"}, {Name: "py39-coremltools"}, {Name: "py310-coremltools"}, {Name: "py310-other"}}
	require.Equal(t, "py310-coremltools", CounterpartIn(base, "py313-coremltools"))
	require.Equal(t, "py39-coremltools", CounterpartIn(base, "py39-coremltools"))
	require.Equal(t, "py-coremltools", CounterpartIn(base, "py-coremltools"))
	require.Equal(t, "py313-new", CounterpartIn(base, "py313-new"))
	require.Equal(t, "jq", CounterpartIn(nil, "jq"))
}

// The names MacPorts may give a project's port, by the language
// PortGroups' prefixes.
func TestLikelyPortNames(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"py-sdnotify", "p5-sdnotify", "rb-sdnotify", "R-sdnotify"}, LikelyPortNames("sdnotify"))
	require.Contains(t, LikelyPortNames("python-dateutil"), "py-dateutil")
	require.Contains(t, LikelyPortNames("PyYAML"), "py-pyyaml")
	require.NotContains(t, LikelyPortNames("jq"), "jq")
}
