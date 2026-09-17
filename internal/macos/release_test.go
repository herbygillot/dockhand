package macos

import (
	"github.com/herbygillot/dockhand/internal/record"
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

func TestProductForDarwinDoesNotExtendProvisioning(t *testing.T) {
	for darwin, want := range map[int]string{8: "10.4", 16: "10.12", 19: "10.15", 20: "11", 24: "15", 25: "26"} {
		got, err := ProductForDarwin(darwin)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, err := ProductForDarwin(26)
	require.Error(t, err)
	_, err = ReleaseForDarwin(16)
	require.Error(t, err)
}

func TestDescribeWordsDarwinAsMacOS(t *testing.T) {
	require.Equal(t, "macOS 26 (Tahoe) arm64", Describe(record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}))
	require.Equal(t, "macOS 15 (Sequoia)", Describe(record.Platform{OS: "darwin", Version: "24"}))
	require.Equal(t, "darwin 99 arm64", Describe(record.Platform{OS: "darwin", Version: "99", Architecture: "arm64"}), "an unknown release keeps the raw fields")
	require.Equal(t, "linux 6", Describe(record.Platform{OS: "linux", Version: "6"}))
}
