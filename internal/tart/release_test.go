package tart

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

// A release's images, their golden copies, and their source are named by
// one descriptor, from the release table's slug, so a release added there
// is provisionable without touching this package; and a name reads back as
// the image it names. The Golden Gate source is the one Cirrus Labs
// publishes.
func TestEveryKnownReleaseNamesItsImagesAndSource(t *testing.T) {
	for _, release := range macos.Known() {
		base := Prepared{Release: release, Profile: macos.ProfileTools}
		xcode := Prepared{Release: release, Profile: macos.ProfileXcode}
		require.Equal(t, "dockhand-base-"+release.Slug, base.Name())
		require.Equal(t, "dockhand-golden-"+release.Slug, base.Golden())
		require.Equal(t, "dockhand-xcode-"+release.Slug, xcode.Name())
		require.Equal(t, "dockhand-golden-xcode-"+release.Slug, xcode.Golden())
		require.Equal(t, "ghcr.io/cirruslabs/macos-"+release.Slug+"-vanilla:latest", base.Source())
		require.Equal(t, base.Source(), xcode.Source())
		for _, prepared := range []Prepared{base, xcode} {
			read, ok := ParsePrepared(prepared.Name())
			require.True(t, ok)
			require.Equal(t, prepared, read)
			require.Equal(t, prepared.Golden(), GoldenName(prepared.Name()))
		}
	}
	goldenGate, err := macos.ParseRelease("golden-gate")
	require.NoError(t, err)
	require.Equal(t, "ghcr.io/cirruslabs/macos-golden-gate-vanilla:latest", Prepared{Release: goldenGate}.Source())
	for _, name := range []string{"my-ventura", "dockhand-base-leopard", "dockhand-golden-tahoe", "dockhand-base-tahoe-next"} {
		_, ok := ParsePrepared(name)
		require.False(t, ok, name)
	}
	require.Equal(t, "my-ventura-golden", GoldenName("my-ventura"))
}

// A platform names a release only where Tart can make its image: arm64,
// on a release the table knows.
func TestOnlyAKnownArm64ReleaseIsProvisionable(t *testing.T) {
	_, err := ReleaseForPlatform(model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"})
	require.NoError(t, err)
	for _, platform := range []model.Platform{
		{OS: "linux", Version: "25", Architecture: "arm64"},
		{OS: "darwin", Version: "25", Architecture: "x86_64"},
		{OS: "darwin", Version: "unknown", Architecture: "arm64"},
		{OS: "darwin", Version: "26", Architecture: "arm64"},
	} {
		_, err := ReleaseForPlatform(platform)
		require.Error(t, err)
	}
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
