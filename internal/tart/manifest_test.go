package tart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// An image's origin is what it was made from and with: its source by
// digest, the setup protocol, and its tools and MacPorts. It is unknown
// without the digest, as for an image made before digests were recorded.
func TestAnImagesOriginIsItsSourceAndWhatSetupPutInIt(t *testing.T) {
	manifest := ImageManifest{Protocol: ImageManifestProtocol, Source: "ghcr.io/cirruslabs/macos-tahoe-vanilla:latest", SourceDigest: pinned,
		Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, MacPortsVersion: "2.12.6", CommandLineTools: "26.6.0.0.1781586589", SetupProtocol: 1}
	require.Equal(t, "source "+pinned+"; setup 1; macports 2.12.6; tools 26.6.0.0.1781586589", manifest.Origin())
	xcode := manifest
	xcode.XcodeVersion = "26.3"
	require.NotEqual(t, manifest.Origin(), xcode.Origin(), "Xcode is part of it")
	newer := manifest
	newer.MacPortsVersion = "2.13.0"
	require.NotEqual(t, manifest.Origin(), newer.Origin())
	unpinned := manifest
	unpinned.SourceDigest = ""
	require.Empty(t, unpinned.Origin())
}

// Setup's record of an image is kept in dockhand's own directory, beside
// its locks, keyed by the Tart home, and read back as written.
func TestAnImagesRecordIsKeptBesideTheTartHome(t *testing.T) {
	user := t.TempDir()
	t.Setenv("HOME", user)
	t.Setenv("DOCKHAND_TART_HOME", "")
	home := filepath.Join(user, "tart")
	require.NoError(t, os.MkdirAll(home, 0o700))
	_, found, err := ReadImageRecord(home, "dockhand-base-tahoe")
	require.NoError(t, err)
	require.False(t, found)

	manifest := ImageManifest{Protocol: ImageManifestProtocol, Source: "ghcr.io/cirruslabs/macos-tahoe-vanilla:latest", SourceDigest: pinned, MacPortsVersion: "2.12.6", SetupProtocol: SetupProtocol}
	require.NoError(t, WriteImageRecord(home, "dockhand-base-tahoe", manifest))
	got, found, err := ReadImageRecord(home, "dockhand-base-tahoe")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, manifest, got)
	directory, err := ImageRecordDirectory(home)
	require.NoError(t, err)
	locks, err := LockDirectory(home)
	require.NoError(t, err)
	canonical, err := filepath.EvalSymlinks(user)
	require.NoError(t, err)
	require.Equal(t, filepath.Base(locks), filepath.Base(directory), "keyed as the locks are")
	require.True(t, filepath.Dir(directory) == filepath.Join(user, ".dockhand", "tart-images") || filepath.Dir(directory) == filepath.Join(canonical, ".dockhand", "tart-images"))
}
