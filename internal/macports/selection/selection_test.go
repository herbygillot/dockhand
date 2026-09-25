package selection_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func indexedTree(t *testing.T, version string) macports.Tree {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sysutils", "fixture"), 0700))
	data := `PortSystem 1.0
name fixture
version ` + version + `
categories sysutils
license MIT
homepage https://example.invalid
description fixture
long_description fixture
subport fixture-1.16 {version 1.16.2}
foreach py {311 312} {subport py${py}-fixture {version 2.0}}
`
	require.NoError(t, os.WriteFile(filepath.Join(root, "sysutils/fixture/Portfile"), []byte(data), 0600))
	var index strings.Builder
	for _, name := range []string{"fixture", "fixture-1.16", "py311-fixture", "py312-fixture"} {
		body := fmt.Sprintf("name %s portdir sysutils/fixture\n", name)
		fmt.Fprintf(&index, "%s %d\n%s", name, len(body), body)
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "PortIndex"), []byte(index.String()), 0600))
	tree, err := macports.NewTree(record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, root, record.Platform{})
	require.NoError(t, err)
	return tree
}

func native(t *testing.T) *eval.Evaluator {
	t.Helper()
	return &eval.Evaluator{Executable: testsupport.MacPortsTclsh(t), Adapter: testsupport.BaseAdapter()}
}

func TestNamedPortsUseOwningSnapshotAndPreserveSiblings(t *testing.T) {
	tree := indexedTree(t, "1.0")
	reader := &selection.Reader{Evaluator: native(t), Index: indexFunc(func(ctx context.Context, bound macports.Tree) (*portindex.Index, error) {
		require.Equal(t, tree.Source(), bound.Source())
		require.Equal(t, tree.Root(), bound.Root())
		return portindex.Open(bound.Root())
	})}
	for _, test := range []struct{ name, subport, version string }{{"fixture", "", "1.0"}, {"fixture-1.16", "fixture-1.16", "1.16.2"}, {"py311-fixture", "py311-fixture", "2.0"}} {
		targets, err := reader.Resolve(t.Context(), tree, macports.Selection{Selector: test.name})
		require.NoError(t, err)
		require.Len(t, targets, 1)
		require.Equal(t, test.name, targets[0].Name)
		require.Equal(t, test.subport, targets[0].Subport)
		require.Equal(t, "sysutils/fixture/Portfile", targets[0].Portfile)
		bound, err := tree.Select(targets[0])
		require.NoError(t, err)
		snapshot, err := reader.Evaluate(t.Context(), bound)
		require.NoError(t, err)
		require.Equal(t, test.version, snapshot.Ports[test.name].Version)
		if test.subport == "" {
			require.Len(t, snapshot.Ports, 4)
		}
	}
	other := indexedTree(t, "3.0")
	reader.Index = indexFunc(func(_ context.Context, bound macports.Tree) (*portindex.Index, error) {
		return portindex.Open(bound.Root())
	})
	targets, err := reader.Resolve(t.Context(), other, macports.Selection{Selector: "fixture"})
	require.NoError(t, err)
	bound, err := other.Select(targets[0])
	require.NoError(t, err)
	snapshot, err := reader.Evaluate(t.Context(), bound)
	require.NoError(t, err)
	require.Equal(t, "3.0", snapshot.Ports["fixture"].Version)
}

func TestMissingOrStaleNamesNeverFallBackToParent(t *testing.T) {
	tree := indexedTree(t, "1.0")
	reader := &selection.Reader{Evaluator: native(t), Index: indexFunc(func(_ context.Context, tree macports.Tree) (*portindex.Index, error) {
		return portindex.Open(tree.Root())
	})}
	_, err := reader.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture-1.17"})
	require.ErrorIs(t, err, portindex.ErrNotIndexed)
	body := "name fixture-1.17 portdir sysutils/fixture\n"
	require.NoError(t, os.WriteFile(filepath.Join(tree.Root(), "PortIndex"), []byte(fmt.Sprintf("fixture-1.17 %d\n%s", len(body), body)), 0600))
	_, err = reader.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture-1.17"})
	require.Error(t, err)
	body = "name fixture portdir sysutils/../sysutils/fixture\n"
	require.NoError(t, os.WriteFile(filepath.Join(tree.Root(), "PortIndex"), []byte(fmt.Sprintf("fixture %d\n%s", len(body), body)), 0600))
	_, err = reader.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture"})
	require.ErrorIs(t, err, macports.ErrTarget)
}

// indexFunc is a fixture index source: the test opens what it wrote.
type indexFunc func(context.Context, macports.Tree) (*portindex.Index, error)

func (f indexFunc) Index(ctx context.Context, tree macports.Tree) (*portindex.Index, error) {
	return f(ctx, tree)
}
