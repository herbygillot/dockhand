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

// drivers are the engine's files that carry work through its stages, as
// the commands do: serve, its submissions, ship's engine path and
// recovery. They build no stage's request themselves.
var drivers = []string{"internal/engine/serve.go", "internal/engine/servesubmit.go", "internal/engine/ship.go", "internal/engine/recovery.go"}

// stageLiteralsAllowed are the named exceptions, each with why: the
// primitives keep their literals, and check, submit and review on their
// own build theirs from their own flags.
var stageLiteralsAllowed = map[string]string{
	"internal/command/author/author.go": "the checksum refresh, a primitive",
	"internal/command/author/edit.go":   "revbump, a primitive",
	"internal/command/check/":           "check on its own, which reads the kept place and never changes the request",
	"internal/command/submit/":          "submit on its own, a person's, which takes no request",
	"internal/command/review/":          "review's check of another's pull request, which has no request",
}

// The change request is made only by the resolver, the store's decoding
// and tests; the stages' requests are built from it, never by a command
// or a driver, but for the named primitives and check, submit and review
// on their own (#195, "The boundary test").
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
		command := strings.HasPrefix(rel, "internal/command/")
		driver := slices.Contains(drivers, rel)
		allowed := false
		for prefix := range stageLiteralsAllowed {
			allowed = allowed || strings.HasPrefix(rel, prefix)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			literal, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			name := typeName(literal.Type)
			if name == "ChangeRequest" && rel != "internal/engine/resolve.go" && !strings.HasPrefix(rel, "internal/store/") && !strings.HasPrefix(rel, "internal/model/") {
				t.Errorf("%s builds a ChangeRequest; only the resolver does", rel)
			}
			if slices.Contains(stageRequests, name) && (command || driver) && !allowed {
				t.Errorf("%s builds a %s; the engine builds it from the request (%sFor)", rel, name, name)
			}
			return true
		})
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
