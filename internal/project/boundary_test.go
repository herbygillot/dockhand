package project

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// projectImports are the packages of dockhand's own this package may
// import, each with why.
var projectImports = map[string]string{
	"internal/archive": "an archive's members, which a reading walks",
}

// The project reader knows nothing of MacPorts, nor of anything that
// decides with what it reads: packages that know ports map their facts
// onto its, never the other way (the assessment design, B, and its
// critique's point 8).
func TestTheProjectReaderKnowsNothingOfPorts(t *testing.T) {
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
			if _, allowed := projectImports[local]; ours && !allowed {
				t.Errorf("%s imports %s: the project reader knows only upstream's files", name, path)
			}
		}
	}
}
