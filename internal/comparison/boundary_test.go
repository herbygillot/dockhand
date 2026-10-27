package comparison

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// comparisonImports are the packages of dockhand's own this package may
// import, each with why.
var comparisonImports = map[string]string{
	"internal/archive":             "an archive that's a binary package, which holds no source",
	"internal/editprep":            "the update's preparation, whose archives an update's comparison reads",
	"internal/macports":            "a port as evaluated, its fetch plan, and its source set",
	"internal/macports/assess":     "the rules applied to what was read",
	"internal/macports/distfetch":  "fetching a fetch plan's archives, and the mirror",
	"internal/macports/patchcheck": "checking a port's patches against the new source",
	"internal/macports/portfile":   "a Portfile's declared checksums",
	"internal/macports/treequery":  "fetching planned archives, and a planner's errors",
	"internal/model":               "the comparison it returns",
	"internal/project":             "reading a project's files, and the readings cache",
	"internal/refusal":             "a problem's kind, which says whether another try may meet it",
	"internal/scratch":             "the scratch directory archives are fetched into",
	"internal/sourcecompare":       "the versions compared",
}

// The comparison reads through what its Collector is given: no store, no
// Git repository, and nothing of the engine, which records what it returns
// (Then's item 15, the architecture thread's shape).
func TestTheComparisonReadsOnlyThroughItsCollector(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	files := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, name, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			require.NoError(t, err)
			local, ours := strings.CutPrefix(path, "github.com/herbygillot/dockhand/")
			if _, allowed := comparisonImports[local]; ours && !allowed {
				t.Errorf("%s imports %s: the comparison reads through its Collector", name, path)
			}
		}
	}
}
