package check

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// check's --variants is each, or variants as MacPorts' command line takes
// them.
func TestTheVariantsFlag(t *testing.T) {
	t.Parallel()
	variants, each, err := variantsFlag("+tests -docs")
	require.NoError(t, err)
	require.False(t, each)
	require.Equal(t, map[string]bool{"tests": true, "docs": false}, variants)
	variants, each, err = variantsFlag(" each ")
	require.NoError(t, err)
	require.True(t, each)
	require.Nil(t, variants)
	variants, each, err = variantsFlag("")
	require.NoError(t, err)
	require.False(t, each)
	require.Nil(t, variants)
	_, _, err = variantsFlag("tests")
	require.ErrorContains(t, err, "--variants: ")
}
