package eval

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// The evaluator's call bound, CallBound, held against its heaviest real
// calls, in a ports tree DOCKHAND_TEST_PORTS_TREE names: starting a
// session on the tree, its initialize among it, and evaluating one
// subport of the largest families, lang/php's 605 and
// textproc/tesseract's 101, whose Portfiles loop over the whole family
// for each. Each call is a tenth of the bound or less, so the bound ends
// only a call that hung (batch 26's leftover). A family's whole
// evaluation is its subports' calls, each bounded alone, and is said.
func TestTheCallBoundHoldsForTheHeaviestRealCalls(t *testing.T) {
	root := os.Getenv("DOCKHAND_TEST_PORTS_TREE")
	if root == "" {
		t.Skip("DOCKHAND_TEST_PORTS_TREE names no ports tree")
	}
	e := liveEvaluator(t)
	tree, err := macports.NewTree(model.Source{Tree: model.ObjectID(strings.Repeat("e", 40))}, root, model.Platform{})
	require.NoError(t, err)
	margin := CallBound / 10

	began := time.Now()
	session, err := e.Open(t.Context(), tree)
	require.NoError(t, err)
	defer session.Close()
	opened := time.Since(began)
	t.Logf("a session started on the tree, initialize among it, in %s", opened.Round(time.Millisecond))
	require.Less(t, opened, margin)

	for _, family := range []struct{ directory, port, last string }{{"lang/php", "php", "php52-xsl"}, {"textproc/tesseract", "tesseract", "tesseract-yid"}} {
		for _, name := range []string{family.port, family.last} {
			targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: family.directory, Subport: name})
			require.NoError(t, err, name)
			source, err := tree.Select(targets[0])
			require.NoError(t, err)
			began = time.Now()
			snapshot, err := session.EvaluateSelected(t.Context(), source)
			took := time.Since(began)
			require.NoError(t, err, name)
			require.Contains(t, snapshot.Ports, name)
			t.Logf("%s evaluated in %s", name, took.Round(time.Millisecond))
			require.Less(t, took, margin, name)
		}
		targets, err := e.Resolve(t.Context(), tree, macports.Selection{Selector: family.directory})
		require.NoError(t, err)
		source, err := tree.Select(targets[0])
		require.NoError(t, err)
		began = time.Now()
		snapshot, err := session.Evaluate(t.Context(), source)
		require.NoError(t, err)
		t.Logf("%s's family of %d evaluated in %s, a call each", family.port, len(snapshot.Ports), time.Since(began).Round(time.Millisecond))
	}
}
