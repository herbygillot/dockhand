package model

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// model is the records every layer shares, so it imports none of
// dockhand's packages: one it did would be imported by all of them (the
// architecture review's X4).
func TestModelImportsNothingOfDockhands(t *testing.T) {
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
			if strings.HasPrefix(path, "github.com/herbygillot/dockhand/") {
				t.Errorf("%s imports %s: model's records import nothing of dockhand's", name, path)
			}
		}
	}
}
