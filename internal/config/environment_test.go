package config_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/config"
)

// The catalogue is what usage.md's table lists, and what its sentence on
// serve --install's agent names.
func TestTheEnvironmentIsTheOneUsageLists(t *testing.T) {
	usage, err := os.ReadFile(filepath.Join("..", "..", "docs", "usage.md"))
	require.NoError(t, err)
	section := string(usage)
	section = section[strings.Index(section, "### Environment"):]
	table := section[:strings.Index(section, "`serve --install` keeps")]
	var listed []string
	for _, row := range regexp.MustCompile("(?m)^\\| (`[A-Z_]+`(?:, `[A-Z_]+`)*) \\|").FindAllStringSubmatch(table, -1) {
		for _, name := range strings.Split(row[1], ", ") {
			listed = append(listed, strings.Trim(name, "`"))
		}
	}
	var names []string
	for _, variable := range config.Environment {
		names = append(names, variable.Name)
	}
	require.ElementsMatch(t, names, listed, "usage.md's table")

	sentence := section[strings.Index(section, "`serve --install` keeps"):]
	sentence = sentence[:strings.Index(sentence, "\n")]
	var kept []string
	for _, name := range regexp.MustCompile("`(DOCKHAND_[A-Z_]+|TART_HOME)`").FindAllStringSubmatch(sentence, -1) {
		kept = append(kept, name[1])
	}
	require.ElementsMatch(t, config.AgentVariables(), kept, "usage.md's sentence on the agent")
}

// Every variable dockhand's code reads by name is in the catalogue, but
// for PATH, HOME, and XDG_CACHE_HOME, which every program reads.
func TestEveryVariableReadIsCatalogued(t *testing.T) {
	var names []string
	for _, variable := range config.Environment {
		names = append(names, variable.Name)
	}
	files := token.NewFileSet()
	root := filepath.Join("..", "..", "internal")
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(path, "testsupport") {
			return err
		}
		file, err := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "Getenv" && selector.Sel.Name != "getenv" && selector.Sel.Name != "LookupEnv") {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}
			name, err := strconv.Unquote(literal.Value)
			require.NoError(t, err)
			if name == "PATH" || name == "HOME" || name == "XDG_CACHE_HOME" || name == "DOCKHAND_FAILPOINT" || strings.HasPrefix(name, "DOCKHAND_TEST_") {
				return true
			}
			require.True(t, slices.Contains(names, name), "%s reads %s, which config.Environment doesn't list", files.Position(call.Pos()), name)
			return true
		})
		return nil
	}))
}
