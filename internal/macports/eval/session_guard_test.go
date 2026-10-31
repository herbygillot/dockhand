package eval

import (
	"iter"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

// A shared interpreter session must evaluate exactly what a fresh
// interpreter evaluates, port after port: the guard for reusing one session
// across a preparation's native evaluations, and a port's resolution and
// version comparisons with them, in either order. The fixture tree always
// runs; DOCKHAND_GUARD_TREE names a ports tree whose Portfiles are sampled
// too.
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
	corpus, err := macports.NewTree(model.Source{Tree: model.ObjectID(tree.Source().Tree)}, root, model.Platform{})
	require.NoError(t, err)
	compareSessions(t, evaluator, corpus, selections)
}

func compareSessions(t *testing.T, evaluator *Evaluator, tree macports.Tree, selections []macports.Selection) {
	t.Helper()
	compareSessionsInOrder(t, evaluator, tree, slices.All(selections))
	compareSessionsInOrder(t, evaluator, tree, slices.Backward(selections))
}

func compareSessionsInOrder(t *testing.T, evaluator *Evaluator, tree macports.Tree, selections iter.Seq2[int, macports.Selection]) {
	t.Helper()
	shared, err := evaluator.Open(t.Context(), tree)
	require.NoError(t, err)
	defer shared.Close()
	versions := shared.Versions()
	for _, selection := range selections {
		targets, err := evaluator.Resolve(t.Context(), tree, selection)
		resolved, resolvedErr := shared.Resolve(t.Context(), tree, selection)
		require.Equal(t, err == nil, resolvedErr == nil, "%s: fresh %v, shared %v", selection.Selector, err, resolvedErr)
		if err != nil || len(targets) != 1 {
			continue
		}
		require.Equal(t, targets, resolved, selection.Selector)
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
		// The port's version against a newer one, compared between its
		// evaluations as a discovery does.
		version := fresh.Ports[targets[0].Name].Version
		want, err := evaluator.CompareVersions(t.Context(), version, version+".1")
		require.NoError(t, err)
		got, err := versions.CompareVersions(t.Context(), version, version+".1")
		require.NoError(t, err)
		require.Equal(t, want, got, selection.Selector)
	}
	// Closing the comparisons leaves the session open.
	require.NoError(t, versions.Close())
	_, err = versions.CompareVersions(t.Context(), "1", "2")
	require.NoError(t, err)
}
