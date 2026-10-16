package macports

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// macportsImports are the packages of dockhand's own this package may
// import, each with why: it holds facts about MacPorts every layer shares,
// so it knows nothing of the engine, the store, or a provider (the
// architecture review's X4).
var macportsImports = map[string]string{
	"internal/model":      "the platforms and records its facts are about",
	"internal/macos":      "the macOS release a Darwin version is, which MacPorts' platforms name",
	"internal/project":    "the build systems and licenses a PortGroup or license line names",
	"internal/tcl/syntax": "reading the Tcl words a Portfile's options are",
	"internal/textedit":   "the spans a maintainers line is edited at",
}

func TestMacPortsImportsOnlyFacts(t *testing.T) {
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
			if _, allowed := macportsImports[local]; ours && !allowed {
				t.Errorf("%s imports %s: name the import and why in macportsImports, or move what it serves out of macports", name, path)
			}
		}
	}
}

// Below macports: none of its packages reaches up to what runs dockhand,
// the store, coordination, the engine, the command layer, or a provider,
// which would carry their dependencies into every reader of a Portfile
// (the architecture review's X4).
func TestTheMacPortsPackagesStayBelowTheEngine(t *testing.T) {
	above := []string{"internal/store", "internal/coord", "internal/engine", "internal/command", "internal/buildenv"}
	files := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(files, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			local := strings.TrimPrefix(imported, "github.com/herbygillot/dockhand/")
			for _, layer := range above {
				if local == layer || strings.HasPrefix(local, layer+"/") {
					t.Errorf("macports/%s imports %s, above macports", path, imported)
				}
			}
		}
		return nil
	})
	require.NoError(t, err)
}
