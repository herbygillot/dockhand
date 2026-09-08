package edit

// Every producer stamps a kind, proved over the source rather than over
// a run.
//
// A behavioural test can only reach the producers a fixture can drive,
// and the edit producers are the least reachable code in the tree: the
// toolchain floor wants a go.mod extracted from a fetched tarball, the
// cargo git-crates block wants revisions fetched from a forge. A sweep
// that missed those would be a sweep that says "every construction site"
// while meaning "the six with harnesses".
//
// So this reads the lines the compiler sees. Unclassified is the refused
// zero, and the way a producer leaves it is not by writing
// `Kind: edit.Unclassified` — nobody does that — but by writing an
// edit.Edit literal with no Kind field at all. That omission is a
// SYNTACTIC fact, so it is caught syntactically, in one place, for every
// package at once, including the ones a future step adds.
//
// The precedent is internal/verdict's ruling index, which pins the
// structural facts it cannot exercise the same way.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEveryProducerStampsAKind(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	stamped := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// vendor/ is other people's code, and a dot directory is not
			// dockhand's source.
			if d.Name() == "vendor" || (path != root && strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		// Tests are exempt on purpose: a test that builds a bare edit to
		// exercise Apply's span arithmetic is not a producer, and forcing
		// a kind on it would make the fixtures claim a provenance the
		// case is not about.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			// A file the parser cannot read is a compile failure this test
			// is not the right one to report.
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		for _, lit := range editLiterals(file) {
			// `edit.Edit{}` with no fields is the empty value returned
			// beside an error or a false ok, never an edit anyone applies.
			if len(lit.Elts) == 0 {
				continue
			}
			if assert.True(t, stampsKind(lit),
				"%s:%d: an edit.Edit literal with no Kind — Unclassified is the refused zero",
				rel, fset.Position(lit.Lbrace).Line) {
				stamped++
			}
		}
		return nil
	})
	require.NoError(t, err)

	// A sweep that matched nothing would pass silently, and a sweep is
	// exactly the shape that can stop matching without anyone noticing —
	// a rename of the import alias, a producer moved behind a
	// constructor. The floor is the count at the time the kind was
	// introduced, so it fails loudly if the walk goes blind.
	assert.GreaterOrEqual(t, stamped, 14,
		"the sweep found %d stamped edits; it used to find 14, so it has stopped seeing producers", stamped)
}

// editLiterals collects every composite literal of type edit.Edit in a
// file, INCLUDING the elided ones inside a []edit.Edit literal, which is
// how three of the producers spell theirs: `[]edit.Edit{{Start: ...}}`
// has an inner literal with no type of its own.
func editLiterals(file *ast.File) []*ast.CompositeLit {
	var out []*ast.CompositeLit
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch typ := lit.Type.(type) {
		case *ast.SelectorExpr:
			if isEditEdit(typ) {
				out = append(out, lit)
			}
		case *ast.ArrayType:
			sel, ok := typ.Elt.(*ast.SelectorExpr)
			if !ok || !isEditEdit(sel) {
				return true
			}
			for _, el := range lit.Elts {
				if inner, ok := el.(*ast.CompositeLit); ok && inner.Type == nil {
					out = append(out, inner)
				}
			}
		}
		return true
	})
	return out
}

func isEditEdit(sel *ast.SelectorExpr) bool {
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "edit" && sel.Sel.Name == "Edit"
}

// stampsKind requires the field by name. A positional literal would slip
// past a name check, so it is refused outright rather than read: nothing
// in the tree writes one, and an edit's field order is not a thing to
// depend on.
func stampsKind(lit *ast.CompositeLit) bool {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			return false
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Kind" {
			return true
		}
	}
	return false
}

// moduleRoot walks up from the package directory to the go.mod beside
// the tree this test sweeps.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "no go.mod above %s", dir)
		dir = parent
	}
}
