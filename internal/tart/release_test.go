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
		{OS: "darwin", Version: "26", Architecture: "arm64"},
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
	platform := record.Platform{OS: "darwin", Version: "27", Architecture: "arm64"}
	source, err := DefaultSource(platform)
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/cirruslabs/macos-golden-gate-vanilla:latest", source)
}

// The releases a local image serves are read from setup's image names, a
// command-line-tools or a full-Xcode image either; a pulled OCI image or an
// image under another name serves none.
func TestPreparedReleasesAreSetupsLocalImages(t *testing.T) {
	images := []Image{
		{Name: "dockhand-xcode-sequoia", Source: "local"},
		{Name: "dockhand-base-sonoma", Source: "local"},
		{Name: "dockhand-base-sequoia", Source: "local"},
		{Name: "dockhand-base-tahoe", Source: "OCI"},
		{Name: "my-ventura", Source: "local"},
		{Name: "dockhand-base-ventura-golden", Source: "local"},
	}
	var slugs []string
	for _, release := range PreparedReleases(images) {
		slugs = append(slugs, release.Slug)
	}
	require.Equal(t, []string{"sonoma", "sequoia"}, slugs, "oldest first, each once")
	require.Empty(t, PreparedReleases(nil))
}
