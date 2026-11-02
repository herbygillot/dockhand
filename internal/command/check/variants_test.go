package check

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// check's --variants is each, or variants as MacPorts' command line takes
// them.
func TestTheVariantsFlag(t *testing.T) {
	t.Parallel()
	port, variants, each, err := variantsFlag("+tests -docs")
	require.Empty(t, port)
	require.NoError(t, err)
	require.False(t, each)
	require.Equal(t, map[string]bool{"tests": true, "docs": false}, variants)
	_, variants, each, err = variantsFlag(" each ")
	require.NoError(t, err)
	require.True(t, each)
	require.Nil(t, variants)
	_, variants, each, err = variantsFlag("")
	require.NoError(t, err)
	require.False(t, each)
	require.Nil(t, variants)
	_, _, _, err = variantsFlag("tests")
	require.ErrorContains(t, err, "--variants: ")

	// A bare word first names the port they build (field testing's curl
	// +sectrust notes, note 1).
	port, variants, each, err = variantsFlag("curl +gnutls +sectrust")
	require.NoError(t, err)
	require.Equal(t, "curl", port)
	require.False(t, each)
	require.Equal(t, map[string]bool{"gnutls": true, "sectrust": true}, variants)
	port, variants, each, err = variantsFlag("curl each")
	require.NoError(t, err)
	require.Equal(t, "curl", port)
	require.True(t, each)
	require.Nil(t, variants)
	_, _, _, err = variantsFlag("curl")
	require.EqualError(t, err, `--variants: "curl" names a port and no variants after it; a variant is +name or -name, as --variants 'curl +name'`)
}
