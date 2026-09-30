package project

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The requirement is the go directive as go.mod writes it, whatever the
// toolchain directive suggests: the tbls run's 1.26.8 was written 1.26.
func TestAGoModulesGoIsItsDirectiveAsWritten(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ manifest, want string }{
		{"module example.com/x\ngo 1.24\n", "1.24"},
		{"module example.com/x\ngo 1.24.0\n", "1.24.0"},
		{"module example.com/x\ngo 1.26.8\n", "1.26.8"},
		{"module example.com/x\ngo 1.24\ntoolchain go1.25.1\n", "1.24"},
		{"module example.com/x\ngo 1.26\ntoolchain go1.25.1\n", "1.26"},
		{"module example.com/x\n", ""},
	} {
		got, err := ReadGoMod([]byte(test.manifest))
		require.NoError(t, err, test.manifest)
		require.Equal(t, test.want, got.Go, test.manifest)
	}
	_, err := ReadGoMod([]byte("go 1.24 1.25\n"))
	require.Error(t, err)
}

// The binary `go build` makes is named for the module, less a major
// version's suffix; a go.mod naming no module makes none. Its
// requirements are read with which are indirect, and a statement a later
// Go writes is passed over where the reading is lax.
func TestAGoModulesBinaryAndRequirements(t *testing.T) {
	t.Parallel()
	for manifest, want := range map[string]string{"module github.com/jorgerojas26/lazysql\n": "lazysql", "module example.org/tool/v3\n": "tool", "go 1.24\n": ""} {
		module, err := ReadGoMod([]byte(manifest))
		require.NoError(t, err)
		binary, ok := module.Binary()
		require.Equal(t, want, binary)
		require.Equal(t, want != "", ok)
	}
	data := []byte("module m\ngo 1.24\nfuture thing\nrequire (\n\ta v1.0.0\n\tb v1.2.0 // indirect\n)\n")
	_, err := ReadGoMod(data)
	require.Error(t, err)
	module, err := ReadGoModLax(data)
	require.NoError(t, err)
	require.Equal(t, []GoRequire{{Path: "a", Version: "v1.0.0"}, {Path: "b", Version: "v1.2.0", Indirect: true}}, module.Requires)
}
