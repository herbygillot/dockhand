package tart

import (
	"strconv"
	"testing"

	"github.com/herbygillot/dockhand/internal/macos"
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
		{OS: "darwin", Version: "27", Architecture: "arm64"},
	} {
		_, err := DefaultImageName(platform)
		require.Error(t, err)
	}
}

// Image and vanilla-source names are spelled from the release table's slug, so
// a release added there is provisionable without touching this package. The
// Golden Gate source name is the one Cirrus Labs publishes.
func TestEveryKnownReleaseNamesItsImagesAndSource(t *testing.T) {
	for _, release := range macos.Known() {
		platform := record.Platform{OS: "darwin", Version: strconv.Itoa(release.Darwin), Architecture: "arm64"}
		image, err := DefaultImageName(platform)
		require.NoError(t, err, release.Name)
		require.Equal(t, "dockhand-base-"+release.Slug, image)
		source, err := DefaultSource(platform)
		require.NoError(t, err)
		require.Equal(t, "ghcr.io/cirruslabs/macos-"+release.Slug+"-vanilla:latest", source)
	}
	platform := record.Platform{OS: "darwin", Version: "26", Architecture: "arm64"}
	source, err := DefaultSource(platform)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/cirruslabs/macos-golden-gate-vanilla:latest", source)
}

// The release Tart builds on unasked is named here, not taken from whichever
// entry the macOS table happens to carry last. A release added there becomes
// selectable; adopting it is a decision made in this package.
func TestTheDefaultBuildReleaseIsNamedNotTheNewest(t *testing.T) {
	def, err := DefaultRelease()
	require.NoError(t, err)
	require.Equal(t, "Tahoe", def.Name)
	require.False(t, NewerThanDefault(def))
	known := macos.Known()
	if newest := known[len(known)-1]; newest.Darwin != DefaultDarwin {
		require.True(t, NewerThanDefault(newest), "%s is past the default and is not adopted unasked", newest.Name)
	}
}
