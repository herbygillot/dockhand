package macos

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReleaseForDarwin(t *testing.T) {
	release, err := ReleaseForDarwin(25)
	require.NoError(t, err)
	require.Equal(t, Release{Darwin: 25, Product: "26", Name: "Tahoe", Slug: "tahoe"}, release)
	_, err = ReleaseForDarwin(0)
	require.ErrorContains(t, err, "unknown release")
}
