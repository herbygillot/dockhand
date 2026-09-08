package statestore

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// THE ONE-CALLER INVARIANT, AS A TEST RATHER THAN A SENTENCE.
//
// R23's claim is that the store's commit is the ONLY mover of the three
// refs dockhand is the authority for — refs/dockhand/state, the pins
// under refs/dockhand/verify/, and the branches under
// refs/heads/dockhand/. That claim was true in prose for two rulings and
// false in the code for both of them: the design carried THREE
// ref-creating roads (the shipped Repo.Mint, a change.Bind over
// git.CASRef, and a snapshot's pin written before any record), and
// "record first" was a convention each road re-implemented and the third
// forgot.
//
// A claim about WHO MAY CALL something is a claim Go cannot state. There
// is no visibility rule that says "package git exports this to exactly
// one package", and making UpdateRefs unexported would put the store
// inside internal/git, which is where the layering ends. So the census
// says it: one batch verb, one caller, and the owned ref literals only
// where the store that judges them lives. The design's harness spells
// this as check_callers.py; this is the same census in the repository's
// own language, run by `go test`, so it re-proves itself on every build
// rather than on a maintainer's memory.
//
// It walks the AST rather than the text, which is stronger than the
// harness it ports: a doc comment may NAME update-ref (this package's do,
// repeatedly, because they explain what the batch is made of), and only
// a call expression is a call and only a string literal is a literal.

// theMover is the package allowed to call git.UpdateRefs. It is a list
// of one plus the fixtures, and both entries are rulings rather than
// conveniences — a third entry is a change to R23 and belongs in a
// ruling before it belongs here.
//
// internal/git/gittest is the exception and it is a narrow one: it is a
// package of test fixtures, it is linked into no binary, and what it
// does with a batch is land a branch in a throwaway repository so that
// the packages under test have something to read. It is what `git
// branch` would be if a fixture were allowed to use porcelain.
var theMover = map[string]bool{
	"internal/statestore":  true,
	"internal/git/gittest": true,
}

// ownedLiterals are the three namespaces the store is the authority for.
// A literal under one of them, outside the packages that own the names,
// is a ref writer in waiting: somebody about to spell a ref rather than
// ask for one.
//
// internal/change is on this list because it has landed —
// change.BranchRef and change.PinRef are the public spellings, and the
// store's own copies here are the boundary it judges names against.
// Nothing else may carry one, and a third entry is a change to R23
// before it is a change to this map.
var ownedLiterals = map[string]bool{
	"internal/statestore": true,
	"internal/change":     true,
}

// ownedPrefixes is what a ref literal is measured against. The state ref
// is one exact name; the other two are namespaces, and a literal that
// merely starts with one is enough to flag — a package spelling
// "refs/heads/dockhand/" has already decided to write a branch itself.
var ownedPrefixes = []string{Ref, pinNamespace, branchNamespace}

func TestUpdateRefsHasExactlyOneCaller(t *testing.T) {
	calls := map[string]int{}
	walk(t, func(pkg, file string, node ast.Node, fset *token.FileSet) {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "UpdateRefs" {
			return
		}
		calls[pkg]++
		assert.True(t, theMover[pkg],
			"%s calls UpdateRefs — statestore.Amend is its only caller (R23: the store's commit is the only mover of dockhand's authority refs)",
			fset.Position(call.Pos()))
	})

	require.NotZero(t, calls["internal/statestore"],
		"the store does not call the verb it is the only caller of — a census of nothing proves nothing")
	assert.Equal(t, 1, calls["internal/statestore"],
		"exactly one call, in Amend: a second would be a second batch, and the batch is what makes the record and the ref one act")
	// The census must be able to SEE a call outside the store, or it
	// passes vacuously and proves nothing about anything. gittest's one
	// fixture call is the standing proof that the walk finds call sites
	// where they are: if this ever reads zero, the finder is broken —
	// and if gittest stops calling the verb, its exception above goes
	// with this line.
	require.NotZero(t, calls["internal/git/gittest"],
		"the census found no call outside the store at all; a walk that finds nothing cannot fail")
}

func TestOnlyTheStoreSpellsAnOwnedRef(t *testing.T) {
	found := 0
	walk(t, func(pkg, file string, node ast.Node, fset *token.FileSet) {
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return
		}
		for _, prefix := range ownedPrefixes {
			if strings.HasPrefix(value, prefix) {
				found++
				assert.True(t, ownedLiterals[pkg],
					"%s carries the ref literal %q — only the store (and, when it lands, change's BranchRef and PinRef) may name a ref dockhand is the authority for",
					fset.Position(lit.Pos()), value)
			}
		}
	})
	// The same tripwire as above: the store spells all three of its own
	// names, so a census that matched none of them is matching nothing.
	require.NotZero(t, found, "the census matched no owned ref literal anywhere, including the store's own")
}

// walk parses every non-test Go file dockhand builds from and calls
// visit for each node, with the file's package path relative to the
// module root.
//
// TEST FILES ARE OUT OF SCOPE, and deliberately: a test plants refs and
// drives batches precisely to prove what the store does with them, and a
// census that flagged those would be measuring its own fixtures. The
// vendored tree is out of scope because it is not this module's code.
//
// It parses rather than type-checks, which matters while the overhaul is
// in flight: several packages are knowingly red — they reference verbs
// later steps delete or rewrite — and a census that needed the tree to
// COMPILE could not run until the last step. Syntax is all a census of
// call sites and literals needs.
func walk(t *testing.T, visit func(pkg, file string, node ast.Node, fset *token.FileSet)) {
	t.Helper()
	root := moduleRoot(t)
	fset := token.NewFileSet()
	seen := 0
	for _, dir := range []string{"internal", "cmd"} {
		require.NoError(t, filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if perr != nil {
				return perr
			}
			rel, rerr := filepath.Rel(root, filepath.Dir(path))
			if rerr != nil {
				return rerr
			}
			seen++
			pkg := filepath.ToSlash(rel)
			ast.Inspect(file, func(node ast.Node) bool {
				if node != nil {
					visit(pkg, path, node, fset)
				}
				return true
			})
			return nil
		}))
	}
	require.Greater(t, seen, 100, "the census walked almost nothing; a census that finds no files is vacuous")
}

// moduleRoot is the directory holding go.mod, found from this source
// file's own compiled-in path rather than from the working directory, so
// the census reads the same tree wherever `go test` was run from.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	require.True(t, ok, "cannot locate this test's own source")
	dir := filepath.Dir(self)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "walked past the filesystem root without finding go.mod")
		dir = parent
	}
}
