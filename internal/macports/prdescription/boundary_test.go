package prdescription

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// prdescriptionImports are the packages of dockhand's own this package may
// import, each with why.
var prdescriptionImports = map[string]string{
	"internal/model":              "what the engine reports of environments and their tools",
	"internal/macports/commitmsg": "dockhand's attribution, which a commit's text the description gives leaves out",
	"internal/buildinfo":          "where dockhand lives, which its first and last lines link",
}

// The description is composed and merged from the facts it's given: it
// reads no store, repository, or provider, and knows nothing of the engine
// or the forge (the architecture review's finding 5).
func TestTheDescriptionIsComposedFromItsFacts(t *testing.T) {
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
			if _, allowed := prdescriptionImports[local]; ours && !allowed {
				t.Errorf("%s imports %s: the description is composed only from its facts", name, path)
			}
		}
	}
}
