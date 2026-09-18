package eval

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// A shared interpreter session must evaluate exactly what a fresh
// interpreter evaluates, port after port: the guard for reusing one session
// across a preparation's native evaluations. The fixture tree always runs;
// DOCKHAND_GUARD_TREE names a ports tree whose Portfiles are sampled too.
func TestSharedSessionEvaluatesLikeFreshInterpreters(t *testing.T) {
	t.Parallel()
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	compareSessions(t, evaluator, tree, []macports.Selection{
		{Selector: "fixture"},
		{Selector: "fixture", Variants: map[string]bool{"debug": true}},
		{Selector: "devel/fixture/Portfile", Subport: "fixture-child"},
		{Selector: "fixture"},
	})
	root := os.Getenv("DOCKHAND_GUARD_TREE")
	if root == "" {
		return
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", "*", "Portfile"))
	require.NoError(t, err)
	sort.Strings(matches)
	var selections []macports.Selection
	step := max(1, len(matches)/40)
	for i := 0; i < len(matches) && len(selections) < 40; i += step {
		relative, err := filepath.Rel(root, matches[i])
		require.NoError(t, err)
		selections = append(selections, macports.Selection{Selector: filepath.ToSlash(relative)})
	}
	corpus, err := macports.NewTree(record.Source{Tree: record.ObjectID(tree.Source().Tree)}, root, record.Platform{})
	require.NoError(t, err)
	compareSessions(t, evaluator, corpus, selections)
}

func compareSessions(t *testing.T, evaluator *Evaluator, tree macports.Tree, selections []macports.Selection) {
	t.Helper()
	shared, err := evaluator.Open(t.Context(), tree)
	require.NoError(t, err)
	defer shared.Close()
	for _, selection := range selections {
		targets, err := evaluator.Resolve(t.Context(), tree, selection)
		if err != nil || len(targets) != 1 {
			continue
		}
		source, err := tree.Select(targets[0])
		require.NoError(t, err)
		fresh, freshErr := evaluator.Evaluate(t.Context(), source)
		reused, reusedErr := shared.Evaluate(t.Context(), source)
		require.Equal(t, freshErr == nil, reusedErr == nil, "%s: fresh %v, shared %v", selection.Selector, freshErr, reusedErr)
		if freshErr != nil {
			continue
		}
		require.Equal(t, fresh.Ports, reused.Ports, selection.Selector)
		freshSelected, err := evaluator.EvaluateSelected(t.Context(), source)
		require.NoError(t, err)
		reusedSelected, err := shared.EvaluateSelected(t.Context(), source)
		require.NoError(t, err)
		require.Equal(t, freshSelected.Ports, reusedSelected.Ports, selection.Selector)
	}
}
