package command

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// outAssignAllowed are the files that may assign a Streams' Out, each with
// why: none, since RunFor reads Prose by a tap (WithProseTap) and no longer
// puts its said-once check under Out.
var outAssignAllowed = map[string]string{}

// No command file assigns a Streams' Out, nor builds Streams over another's
// writer: Out is the raw standard output Begin switched for --json, and
// Prose is built over it, so an Out replaced afterwards, or wrapped and
// put back, leaves Prose writing past it, as serve's stamp missed "serve:
// stopped", or wraps a payload (#94, docs/designs/prose-stream.md at 36675f0d). The
// streams' own methods (Stamped, Staged) change the writers together. The
// check is by syntax: any X.Out assigned is taken as a Streams', which
// no other type in the command layer has today.
func TestNoCommandAliasesStreamsOut(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel := filepath.ToSlash(filepath.Join("internal", "command", path))
		seen[rel] = true
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, rel), nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, problem := range outAliases(file, rel) {
			t.Error(problem)
		}
		return nil
	})
	require.NoError(t, err)
	require.True(t, seen["internal/command/root.go"], "the walk reads the command layer")
	for rel := range outAssignAllowed {
		require.True(t, seen[rel], "%s is named but gone", rel)
	}
}

// outAliases are the places in a file that assign a Streams' Out, or build
// Streams whose Out is another's writer, but where outAssignAllowed names
// the file. Streams built in cmdtest, a test's own from fresh buffers, may
// set Out; a command's are handed to it, and build none.
func outAliases(file *ast.File, rel string) []string {
	var problems []string
	if _, ok := outAssignAllowed[rel]; ok {
		return nil
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if selector, ok := lhs.(*ast.SelectorExpr); ok && selector.Sel.Name == "Out" {
					problems = append(problems, rel+" assigns "+exprText(selector)+"; write through Out or Prose, or change the streams by their own methods (Stamped, Staged)")
				}
			}
		case *ast.CompositeLit:
			if typeName(n.Type) != "Streams" {
				return true
			}
			for _, element := range n.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok || exprText(field.Key) != "Out" {
					continue
				}
				switch {
				case !strings.HasPrefix(rel, "internal/command/cmdtest/"):
					problems = append(problems, rel+" builds Streams with an Out of its own; a command's streams are handed to it")
				case writesThroughStreams(field.Value):
					problems = append(problems, rel+" builds Streams over another's writer, "+exprText(field.Value))
				}
			}
		}
		return true
	})
	return problems
}

// writesThroughStreams reports whether an expression reaches a Streams'
// writer: its Out, its Prose, or what's beneath them.
func writesThroughStreams(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if selector, ok := n.(*ast.SelectorExpr); ok {
			switch selector.Sel.Name {
			case "Out", "Prose", "Beneath":
				found = true
			}
		}
		return !found
	})
	return found
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

// exprText is a short expression as written: names and selectors.
func exprText(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprText(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		return exprText(e.Fun) + "(…)"
	case *ast.UnaryExpr:
		return e.Op.String() + exprText(e.X)
	case *ast.CompositeLit:
		return exprText(e.Type) + "{…}"
	}
	return "…"
}
