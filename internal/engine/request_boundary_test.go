package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// stageRequests are the stages' own requests, built from the change
// request by the engine (#195, "What it replaces").
var stageRequests = []string{"UpdateRequest", "WhereRequest", "CheckRequest", "SubmitRequest"}

// stageLiteralsAllowed are the named exceptions, by file and function,
// each with why: the primitives keep their literals, the engine's place
// for a check no request asks is the settings', and the resolver's
// builders make them. A directory named whole is check's, submit's and
// review's own, which build theirs from their own flags.
var stageLiteralsAllowed = map[string]string{
	"internal/command/author/author.go#ChecksumsCommand": "the checksum refresh, a primitive",
	"internal/command/author/edit.go#RevbumpCommand":     "revbump, a primitive",
	"internal/command/check/":                            "check on its own, which reads the kept place and never changes the request",
	"internal/command/submit/":                           "submit on its own, a person's, which takes no request",
	"internal/command/review/":                           "review's check of another's pull request, which has no request",
	"internal/engine/resolve.go":                         "the resolver and the stages' builders",
	"internal/engine/publish.go#rejudged":                "a stage's submission built again from the request as it stands",
	"internal/engine/create.go#Create":                   "create's checksum update, a primitive",
	"internal/engine/diff.go#RevbumpLinked":              "the dependents' bump, a primitive",
	"internal/engine/update.go#updateWithObsolete":       "the stub's update, a primitive",
	"internal/engine/evidence.go#requiredEnvironments":   "the settings' default place, which no request asks",
	"internal/engine/provider.go#Environments":           "the settings' default place, which no request asks",
}

// allowedAt reports whether a literal in the function named of the file is
// a named exception.
func allowedAt(rel, function string) bool {
	for key := range stageLiteralsAllowed {
		file, fn, named := strings.Cut(key, "#")
		if named && rel == file && fn == function || !named && (rel == file || strings.HasSuffix(file, "/") && strings.HasPrefix(rel, file)) {
			return true
		}
	}
	return false
}

// The change request is made only by the resolver, the store's decoding
// and tests; the stages' requests are built from it, never by a command or
// anywhere else in the engine, but for the named exceptions (#195, "The
// boundary test"; Codex's read of steps 4 and 5).
func TestTheRequestIsBuiltOnlyByTheResolver(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		checked := strings.HasPrefix(rel, "internal/command/") || strings.HasPrefix(rel, "internal/engine/")
		for _, decl := range file.Decls {
			function := ""
			if fn, ok := decl.(*ast.FuncDecl); ok {
				function = fn.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				literal, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				name := typeName(literal.Type)
				if name == "ChangeRequest" && rel != "internal/engine/resolve.go" && !strings.HasPrefix(rel, "internal/store/") && !strings.HasPrefix(rel, "internal/model/") {
					t.Errorf("%s builds a ChangeRequest; only the resolver does", rel)
				}
				if slices.Contains(stageRequests, name) && checked && !allowedAt(rel, function) {
					t.Errorf("%s's %s builds a %s; the engine builds it from the request (%sFor)", rel, function, name, name)
				}
				return true
			})
		}
		return nil
	})
	require.NoError(t, err)
}

// typeName is a composite literal's type's name, its package dropped.
func typeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}
