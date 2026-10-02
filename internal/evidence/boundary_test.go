package evidence

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// evidenceImports are the packages of dockhand's own this package may
// import, each with why.
var evidenceImports = map[string]string{
	"internal/model": "the records it reads",
	"internal/reuse": "the rule by which a build was made against another source (reuse.AgainstOtherSources)",
}

// Evidence combines what it's given: it reads no store, asks no provider,
// and knows nothing of the engine, which loads its records and words what
// it says (the architecture review's finding 4).
func TestEvidenceReadsOnlyWhatItsGiven(t *testing.T) {
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
			if _, allowed := evidenceImports[local]; ours && !allowed {
				t.Errorf("%s imports %s: evidence combines only the records it's given", name, path)
			}
		}
	}
}
