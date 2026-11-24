package assess

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A go.mod's name is its module path's last element without the major
// version, as project's GoModule.Binary reads it: gopkg.in/yaml.v3 is
// yaml, as Go names its package, where assess's own copy once said
// yaml.v3 (#150).
func TestAGoModulesNameIsItsBinarysName(t *testing.T) {
	t.Parallel()
	for data, want := range map[string]string{
		"module gopkg.in/yaml.v3\n":      "yaml",
		"module github.com/a/tool/v2\n":  "tool",
		"module github.com/a/tool/v13\n": "tool",
		"module github.com/a/tool\n":     "tool",
	} {
		name, ok := goModuleName([]byte(data))
		require.True(t, ok, data)
		require.Equal(t, want, name, data)
	}
}
