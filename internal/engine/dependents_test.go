package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/portindex"
)

// dependentsIn finds the direct dependents of the changed directories'
// ports in a port index: each once, with what it depends on and in which
// phases, and none from the changed directories themselves, which a
// revbump of dependents leaves alone (the test plan's step 3).
func TestDependentsAreFoundInTheIndex(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var data strings.Builder
	for _, record := range [][2]string{
		{"libharbor", "name libharbor portdir devel/libharbor"},
		{"libharbor-doc", "name libharbor-doc portdir devel/libharbor depends_lib port:libharbor"},
		{"jq", "name jq portdir textproc/jq depends_lib port:libharbor depends_build port:libharbor-doc"},
		{"yq", "name yq portdir textproc/yq depends_run {port:libharbor path:bin/jq:jq}"},
		{"fd", "name fd portdir sysutils/fd depends_lib port:oniguruma6"},
	} {
		payload := record[1] + "\n"
		fmt.Fprintf(&data, "%s %d\n%s", record[0], len(utf16.Encode([]rune(payload))), payload)
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "PortIndex"), []byte(data.String()), 0o600))
	index, err := portindex.Open(root)
	require.NoError(t, err)

	dependents, err := dependentsIn(index, []string{"devel/libharbor"})
	require.NoError(t, err)
	require.Equal(t, []Dependent{
		{Name: "jq", Directory: "textproc/jq", On: []string{"libharbor", "libharbor-doc"}, Phases: []string{"library", "build"}},
		{Name: "yq", Directory: "textproc/yq", On: []string{"libharbor"}, Phases: []string{"runtime"}},
	}, dependents)

	none, err := dependentsIn(index, []string{"sysutils/fd"})
	require.NoError(t, err)
	require.Empty(t, none)
}
