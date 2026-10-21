package compose

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// environmentReads are the calls that read the process's environment,
// home, or working directory, or start a program that inherits them.
var environmentReads = map[string][]string{
	"os":      {"Getenv", "LookupEnv", "UserHomeDir", "UserCacheDir", "Environ", "Getwd"},
	"os/exec": {"Command", "CommandContext"},
}

// readersBelow are the packages below compose that read the process's
// environment today, with how many such calls each makes. compose
// resolves the environment from a command line's Invocation and hands
// what's read down as fields, and what's handed on as one Env, so this
// shrinks to nothing as each package takes them; a count may only fall.
var readersBelow = map[string]int{
	"archive":            1,
	"buildenv/script":    2,
	"buildenv/tart":      1,
	"git":                2,
	"github":             4,
	"macports/portindex": 1,
	"tart":               7,
	"tart/guestssh":      3,
	"tart/host":          1,
	"tcl/shell":          1,
	// These stay: subprocess is where every program starts, with the Env
	// it's given; forgetest is a test's GitHub; and the acceptance build's
	// failpoints are read where they fire.
	"subprocess":      1,
	"forge/forgetest": 1,
	"failpoint":       2,
}

// Nothing below compose reads the process's environment itself
// (architecture's rule for the Invocation, 2026-10-21): what's read comes
// as fields, and what's handed on as an explicit Env.
func TestNothingBelowComposeReadsTheEnvironment(t *testing.T) {
	found := map[string]int{}
	var where []string
	files := token.NewFileSet()
	root := filepath.Join("..")
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		pkg := filepath.ToSlash(filepath.Dir(path))[len("../"):]
		if pkg == "command" || pkg == "compose" || pkg == "testsupport" || strings.HasPrefix(pkg, "testsupport/") {
			return nil
		}
		file, err := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		names := map[string]string{}
		for _, spec := range file.Imports {
			imported, _ := strconv.Unquote(spec.Path.Value)
			if _, watched := environmentReads[imported]; !watched {
				continue
			}
			name := imported[strings.LastIndex(imported, "/")+1:]
			if spec.Name != nil {
				name = spec.Name.Name
			}
			names[name] = imported
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			imported, ok := names[ident.Name]
			if !ok {
				return true
			}
			for _, read := range environmentReads[imported] {
				if selector.Sel.Name == read {
					found[pkg]++
					where = append(where, fmt.Sprintf("%s: %s.%s", files.Position(selector.Pos()), ident.Name, read))
				}
			}
			return true
		})
		return nil
	}))
	sort.Strings(where)
	for pkg, count := range found {
		require.LessOrEqual(t, count, readersBelow[pkg], "%s reads the process's environment %d times, past the %d allowed; take it from compose as a field or an Env:\n%s", pkg, count, readersBelow[pkg], strings.Join(where, "\n"))
	}
	for pkg, allowed := range readersBelow {
		require.Equal(t, allowed, found[pkg], "%s reads the process's environment %d times now; lower readersBelow to match", pkg, found[pkg])
	}
}
