package branchstate

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// branchstateImports are the packages of dockhand's own this package may
// import, each with why.
var branchstateImports = map[string]string{
	"internal/model":    "the records the facts hold",
	"internal/evidence": "what the checks of the files establish",
	"internal/prose":    "counts and lists worded as everywhere else",
	"internal/refusal":  "the Next type, which every layer that refuses fills",
}

// The verdict reads only the facts it's given: no store, no provider, no
// engine, which gathers them (architecture, 2026-10-22).
func TestTheVerdictReadsOnlyItsFacts(t *testing.T) {
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
			if _, allowed := branchstateImports[local]; ours && !allowed {
				t.Errorf("%s imports %s: the verdict reads only the facts it's given", name, path)
			}
		}
	}
}
