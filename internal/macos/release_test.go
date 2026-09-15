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

func TestParseRelease(t *testing.T) {
	for _, value := range []string{"Sonoma", "sonoma", "14", " SONOMA "} {
		release, err := ParseRelease(value)
		require.NoError(t, err)
		require.Equal(t, 23, release.Darwin)
	}
	for _, value := range []string{"", "23", "14.6", "unknown"} {
		_, err := ParseRelease(value)
		require.Error(t, err)
	}
}
