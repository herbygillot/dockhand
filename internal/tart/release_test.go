package tart

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestNativePlatformSelectsConventionalImageAndVanillaSource(t *testing.T) {
	platform := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	image, err := DefaultImageName(platform)
	require.NoError(t, err)
	require.Equal(t, "dockhand-base-tahoe", image)
	xcodeImage, err := DefaultXcodeImageName(platform)
	require.NoError(t, err)
	require.Equal(t, "dockhand-xcode-tahoe", xcodeImage)
	source, err := DefaultSource(platform)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/cirruslabs/macos-tahoe-vanilla:latest", source)
}

func TestImageDefaultsRejectUnsupportedPlatforms(t *testing.T) {
	for _, platform := range []record.Platform{
		{OS: "linux", Version: "25", Architecture: "arm64"},
		{OS: "darwin", Version: "25", Architecture: "x86_64"},
		{OS: "darwin", Version: "unknown", Architecture: "arm64"},
		{OS: "darwin", Version: "26", Architecture: "arm64"},
	} {
		_, err := DefaultImageName(platform)
		require.Error(t, err)
	}
}
