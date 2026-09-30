package macports

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"

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

// A port whose variants couldn't be read has none it can be said to
// declare: an evaluation error of variants or vinfo is an error, as is a
// requires or conflicts list that doesn't parse, never an empty reading
// (the helper-ownership review's finding 4).
func TestVariantsThatCouldntBeReadAreAnError(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"variants", "vinfo"} {
		port := PortInfo{Name: "demo", Options: map[string]string{"variants": "tests", "vinfo": "tests {}"}, OptionErrors: map[string]string{key: "could not evaluate"}}
		_, err := port.Variants()
		require.ErrorContains(t, err, "evaluating "+key+": could not evaluate", key)
	}
	for _, field := range []string{"requires", "conflicts"} {
		fields := field + " " + syntax.Quote("{")
		port := PortInfo{Name: "demo", Options: map[string]string{"variants": "tests", "vinfo": "tests " + syntax.Quote(fields)}}
		_, err := port.Variants()
		require.ErrorContains(t, err, "demo +tests "+field, field)
	}
	port := PortInfo{Name: "demo", Options: map[string]string{"variants": "tests {", "vinfo": ""}}
	_, err := port.Variants()
	require.ErrorContains(t, err, "isn't a Tcl list")
	none, err := PortInfo{Name: "demo", Options: map[string]string{}}.Variants()
	require.NoError(t, err, "a port evaluated without them declares none")
	require.Empty(t, none)
}
