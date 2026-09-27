package provider

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// providerImports are packages a provider must not import, each with why.
// A provider is composed by the command layer and driven by the engine,
// and meets them only through this package's contract.
var providerImports = map[string]string{
	"internal/engine":  "the engine drives providers; a provider meets it through this contract",
	"internal/command": "the command layer composes providers",
	"internal/store":   "a provider records through Build, never in the store itself",
	"internal/coord":   "sessions and leases are the engine's",
}

const module = "github.com/herbygillot/dockhand/"

// A provider imports the contract, never the engine that drives it (the
// architecture review of 2026-09-27, finding 4). Its tests may.
func TestProvidersMeetTheEngineThroughTheContract(t *testing.T) {
	directories, err := os.ReadDir(".")
	require.NoError(t, err)
	files := token.NewFileSet()
	checked := 0
	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		entries, err := os.ReadDir(directory.Name())
		require.NoError(t, err)
		for _, entry := range entries {
			name := filepath.Join(directory.Name(), entry.Name())
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(files, name, nil, parser.ImportsOnly)
			require.NoError(t, err)
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				require.NoError(t, err)
				for forbidden, why := range providerImports {
					if trimmed := strings.TrimPrefix(path, module); trimmed == forbidden || strings.HasPrefix(trimmed, forbidden+"/") {
						t.Errorf("%s imports %s: %s", name, path, why)
					}
				}
			}
			checked++
		}
	}
	require.NotZero(t, checked, "the providers were found")
}
