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
