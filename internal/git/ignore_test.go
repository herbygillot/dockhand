package git_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// What Git ignores, by a .gitignore at the root or in a port's directory,
// is ignored here as Git would: listed by Ignored, left out of Untracked,
// and not staged by Add or AddAll, as create stages a new port and moves
// one (the person, 2026-10-04).
func TestWhatGitIgnoresIsNeitherListedNorStaged(t *testing.T) {
	repo, _ := portsCheckout(t)
	root := repo.Root
	for name, content := range map[string]string{".gitignore": "*.swp\n", "textproc/jq/.gitignore": "scratch/\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}
	testsupport.Git(t, root, "add", ".gitignore", "textproc/jq/.gitignore")
	testsupport.Git(t, root, "commit", "-q", "-m", "ignore an editor's files")
	for name, content := range map[string]string{"textproc/jq/.Portfile.swp": "swap\n", "textproc/jq/scratch/out.txt": "out\n", "textproc/jq/files/fix.patch": "fix\n"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}

	untracked, err := repo.Untracked(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"textproc/jq/files/fix.patch"}, untracked)
	ignored, err := repo.Ignored(t.Context(), "textproc/jq/.Portfile.swp", "textproc/jq/scratch/out.txt", "textproc/jq/files/fix.patch", "textproc/jq/Portfile")
	require.NoError(t, err)
	require.Equal(t, []string{"textproc/jq/.Portfile.swp", "textproc/jq/scratch/out.txt"}, ignored)
	none, err := repo.Ignored(t.Context(), "textproc/jq/files/fix.patch")
	require.NoError(t, err)
	require.Empty(t, none)

	require.NoError(t, repo.AddAll(t.Context(), "textproc/jq"))
	staged := testsupport.Git(t, root, "diff", "--cached", "--name-only")
	require.Equal(t, "textproc/jq/files/fix.patch", staged, "git add --all leaves out what Git ignores")
}

// Only internal/git runs git: nothing else lists untracked files or stages
// working files by its own means, so what Git ignores is ignored however a
// command gathers files. A package that ran git, or asked it for
// untracked files or to stage them, by its own words, would bypass that.
func TestOnlyThisPackageGathersWorkingFiles(t *testing.T) {
	forbidden := []string{"ls-files", "--others", "--exclude-standard", "check-ignore", "update-index"}
	files := token.NewFileSet()
	// The module's own code, but this package's and the test helpers',
	// which make repositories for tests as a person would.
	skipped := map[string]bool{"../../internal/git": true, "../../internal/testsupport": true, "../../internal/forge/forgetest": true}
	visit := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipped[path] || filepath.Base(path) == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(files, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(n.Value)
				if err != nil {
					return true
				}
				for _, word := range forbidden {
					if value == word {
						t.Errorf("%s: %q: only internal/git asks git for untracked files or stages them", files.Position(n.Pos()), value)
					}
				}
			case *ast.CallExpr:
				selector, ok := n.Fun.(*ast.SelectorExpr)
				if !ok || len(n.Args) == 0 {
					return true
				}
				if pkg, ok := selector.X.(*ast.Ident); !ok || pkg.Name != "exec" || (selector.Sel.Name != "Command" && selector.Sel.Name != "CommandContext") {
					return true
				}
				for _, arg := range n.Args {
					if lit, ok := arg.(*ast.BasicLit); ok && lit.Value == `"git"` {
						t.Errorf("%s: runs git itself; only internal/git runs git", files.Position(n.Pos()))
					}
				}
			}
			return true
		})
		return nil
	}
	for _, root := range []string{"../../cmd", "../../internal", "../../tools"} {
		require.NoError(t, filepath.WalkDir(root, visit))
	}
}
