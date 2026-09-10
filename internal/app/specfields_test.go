package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/run"
)

// TestEverySpecFieldHasAProducer is the guard for a defect this tree has
// now shipped twice.
//
// run.Spec is a contract: the app fills it, EnqueueIn freezes it, the
// plan translates it, and a provider acts on it. A field nobody fills is
// not an unused variable — the compiler cannot see it, because every
// stage downstream reads it perfectly well and simply reads a zero. It
// is a silent lie about what the guest was told.
//
// Ask.FromSource was the first: the whole road existed and no caller set
// one, so refresh-checksums verified its re-derived checksums against a
// binary archive built from the bytes it had just replaced — the one
// thing that flag exists to prevent. Requires was the second, found by
// counting producers rather than by anything failing, and it survived
// the pass that fixed FromSource because it was the field beside it.
//
// So the census is mechanical and it lives here, next to the producers,
// because this package is the only one that may write a Spec from
// nothing. A new field arrives red and its author decides what fills it.
func TestEverySpecFieldHasAProducer(t *testing.T) {
	set := producedSpecFields(t)
	spec := reflect.TypeOf(run.Spec{})
	for i := 0; i < spec.NumField(); i++ {
		name := spec.Field(i).Name
		assert.Contains(t, set, name,
			"run.Spec.%s is declared, carried and read, and no run.Spec literal in this package fills it: "+
				"either produce it or take the field out", name)
	}
}

// producedSpecFields is every field named in a run.Spec composite
// literal in this package's non-test files.
//
// Composite literals only, deliberately. A field filled by a later
// assignment would pass a looser test while leaving the literal — the
// thing a reader checks against the type — silently short.
func producedSpecFields(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	out := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		require.NoError(t, err)
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Spec" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "run" {
				return true
			}
			for _, el := range lit.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if k, ok := kv.Key.(*ast.Ident); ok {
						out[k.Name] = true
					}
				}
			}
			return true
		})
	}
	require.NotEmpty(t, out, "no run.Spec literal was found; this census would pass vacuously")
	return out
}
