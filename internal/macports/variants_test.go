package macports

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Variant choices read as MacPorts' command line takes them, and what a
// port doesn't declare is named.
func TestVariantChoicesReadAsMacPortsTakesThem(t *testing.T) {
	t.Parallel()
	for spec, want := range map[string]map[string]bool{
		"+tests":             {"tests": true},
		"+tests -docs":       {"tests": true, "docs": false},
		"+tests-docs+x11":    {"tests": true, "docs": false, "x11": true},
		"  -universal  ":     {"universal": false},
		"+python312 +no_gcc": {"python312": true, "no_gcc": true},
	} {
		got, err := ParseVariants(spec)
		require.NoError(t, err, spec)
		require.Equal(t, want, got, spec)
	}
	for _, spec := range []string{"", "tests", "+", "+tests +tests", "+tests -tests", "+a/b", "++x"} {
		_, err := ParseVariants(spec)
		require.Error(t, err, spec)
	}
	declared := []Variant{{Name: "tests"}, {Name: "docs"}}
	require.Equal(t, []string{"gui", "x11"}, Undeclared(map[string]bool{"tests": true, "x11": true, "gui": false}, declared))
	require.Empty(t, Undeclared(map[string]bool{"docs": false}, declared))
}
