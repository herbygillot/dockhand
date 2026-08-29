package info

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVariantsCanonical(t *testing.T) {
	a, err := Variants("+x11", "-quartz", "+universal")
	require.NoError(t, err)
	b, err := Variants("+universal", "+x11", "-quartz")
	require.NoError(t, err)
	require.Equal(t, a, b, "selection order changed the canonical form")
	require.Equal(t, []string{"-quartz", "+universal", "+x11"}, a.List())
}

func TestVariantsLastWins(t *testing.T) {
	v, err := Variants("+x11", "-x11")
	require.NoError(t, err)
	require.Equal(t, VariantSet("-x11"), v)
}

func TestVariantsRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"x11", "+", "", "~x11"} {
		_, err := Variants(bad)
		require.ErrorIs(t, err, ErrMalformedSelection, "input %q", bad)
	}
}

func TestZeroValueIsDefault(t *testing.T) {
	var v VariantSet
	require.Nil(t, v.List())
}
