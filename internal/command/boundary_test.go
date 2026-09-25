package command

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// commandImports are the packages beyond the standard library the command
// layer may import (doc.go), each with why.
var commandImports = map[string]string{
	"internal/engine":  "does the work",
	"internal/model":   "the records the engine returns",
	"internal/config":  "the person's settings, turned into the engine's options",
	"internal/coord":   "the command's own session, and who leads",
	"internal/version": "the command's own version",
	"internal/store":   "filter types for engine queries; records are read and written through the engine",
	// Vocabulary the engine's results carry, until it moves to a package
	// of its own (roadmap, Next): the kind of edit, a release, a
	// checksum, and a commit-rule finding, whose explanations explain
	// prints.
	"internal/record":               "vocabulary in engine results",
	"internal/macports/portfile":    "vocabulary in engine results",
	"internal/macports/commitrules": "vocabulary in engine results, and explain's text",
	"internal/provider/actions":     "composition: the github provider",
	"internal/provider/script":      "composition: the command provider",
	"internal/github":               "composition and auth: GitHub's client and login",
	"internal/credential":           "auth: where the login is kept",
	"internal/credential/keychain":  "auth: the macOS Keychain",
	"github.com/spf13/cobra":        "the command line itself",
	"golang.org/x/sys/unix":         "whether a stream is a terminal",
}

const module = "github.com/herbygillot/dockhand/"

// The command layer parses, finds context, prompts, and renders; the
// engine decides (doc.go). It imports nothing past its layer, and it
// reads and writes records only through the engine, never with the
// store's own transactions.
func TestCommandTalksToTheEngine(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	files := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, name, nil, parser.SkipObjectResolution)
		require.NoError(t, err)
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			require.NoError(t, err)
			if !strings.Contains(strings.Split(path, "/")[0], ".") {
				continue // the standard library
			}
			if _, allowed := commandImports[strings.TrimPrefix(path, module)]; !allowed {
				t.Errorf("%s imports %s: move the decision it serves into the engine, or name the import and why in commandImports", name, path)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.SelectorExpr)
			if !ok || (call.Sel.Name != "View" && call.Sel.Name != "Update") {
				return true
			}
			if store, ok := call.X.(*ast.SelectorExpr); ok && store.Sel.Name == "Store" {
				t.Errorf("%s: the store's %s: read and write records through the engine", files.Position(call.Pos()), call.Sel.Name)
			}
			return true
		})
	}
}
