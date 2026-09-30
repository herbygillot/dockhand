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
