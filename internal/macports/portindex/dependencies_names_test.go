package portindex

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// An entry's dependencies are read from its fields as Tcl lists, each
// word's port after its last colon, in the order the fields and words
// come: a braced word is one word, as MacPorts reads it.
func TestAnEntrysDependenciesAreReadAsTclLists(t *testing.T) {
	t.Parallel()
	entry := Entry{Name: "jq", Fields: map[string]string{
		DependsBuild: "port:pkgconfig {path:lib/libz.dylib:zlib}",
		DependsLib:   "port:oniguruma6 bin:python3.13:python313",
		DependsRun:   "port:ignored",
	}}
	require.Equal(t, []string{"pkgconfig", "zlib", "oniguruma6", "python313"}, entry.Dependencies(DependsBuild, DependsLib))
	unreadable := Entry{Name: "x", Fields: map[string]string{DependsLib: "port:a {unclosed"}}
	require.Empty(t, unreadable.Dependencies(DependsLib), "a field that isn't a Tcl list names none")
}
