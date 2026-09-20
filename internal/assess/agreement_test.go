package assess

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/stretchr/testify/require"
)

// The whole-tree survey selects every port by its indexed name. A stub such
// as py-foo redirects to the subport carrying its release, so evaluation
// reports py313-foo; that is the redirection working, not a port the index
// did not name. Before this rule, every stub in the tree read as unsupported
// under --all while assessing as ready on its own.
func TestIndexAgreementHonorsAStubRedirection(t *testing.T) {
	t.Parallel()
	require.NoError(t, indexAgreement("jq", "jq", ""), "an ordinary port evaluates as itself")
	require.NoError(t, indexAgreement("py-black", "py313-black", "py-black"), "the index named the stub; the probe carried its release")
	require.NoError(t, indexAgreement("MoltenVK", "MoltenVK-latest", "MoltenVK"))

	err := indexAgreement("SuiteSparse", "SuiteSparse_config", "")
	require.ErrorIs(t, err, portedit.ErrUnsupported, "no stub was resolved, so the disagreement is real")
	require.ErrorContains(t, err, "indexed subport SuiteSparse; evaluation selected a different port SuiteSparse_config")

	err = indexAgreement("py-black", "py313-black", "py-other")
	require.ErrorIs(t, err, portedit.ErrUnsupported, "a stub that is not the indexed name excuses nothing")
}
