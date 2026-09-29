package portedit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// The current version's archives are fetched under the policy every fetch
// dockhand makes keeps: a fetch the policy leaves to a dedicated preparer,
// such as a Git one, is refused before MacPorts is asked for its plan.
func TestTheShippedPlanKeepsTheFetchPolicy(t *testing.T) {
	t.Parallel()
	input := &sourceInput{target: model.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}, before: macports.Snapshot{Root: t.TempDir()}, info: macports.PortInfo{Name: "fixture", Options: map[string]string{
		"fetch.type": "git", "fetch.has_credentials": "0", "fetch.archive_compatible": "1", "fetch.ignore_sslcert": "0",
	}}}
	_, err := shippedPlan(t.Context(), input)
	require.ErrorIs(t, err, ErrUnsupported)
	require.ErrorContains(t, err, "requires a dedicated preparer")
}
