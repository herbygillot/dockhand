package eval

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

func TestSessionEvaluatesRewrittenContentsWithoutRestarting(t *testing.T) {
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	targets, err := evaluator.Resolve(t.Context(), tree, macports.Selection{Selector: "fixture"})
	require.NoError(t, err)
	source, err := tree.Select(targets[0])
	require.NoError(t, err)
	session, err := evaluator.Open(t.Context(), tree)
	require.NoError(t, err)
	defer session.Close()
	snapshot, err := session.EvaluateSelected(t.Context(), source)
	require.NoError(t, err)
	require.Equal(t, "7.5", snapshot.Ports["fixture"].Version)
	require.NotContains(t, snapshot.Ports, "fixture-child", "selected evaluation omits siblings")

	putFile(t, tree.Root(), "devel/fixture/files/metadata.tcl", "set snapshot_minor 6\n")
	snapshot, err = session.EvaluateSelected(t.Context(), source)
	require.NoError(t, err)
	require.Equal(t, "7.6", snapshot.Ports["fixture"].Version, "each evaluation opens the port afresh")
	snapshot, err = session.Evaluate(t.Context(), source)
	require.NoError(t, err)
	require.Equal(t, "11.2", snapshot.Ports["fixture-child"].Version)

	other := fixtureTree(t)
	foreign, err := other.Select(targets[0])
	require.NoError(t, err)
	_, err = session.Evaluate(t.Context(), foreign)
	require.ErrorIs(t, err, macports.ErrTarget)
	require.NoError(t, session.Close())
	_, err = session.Evaluate(t.Context(), source)
	require.Error(t, err)
}
