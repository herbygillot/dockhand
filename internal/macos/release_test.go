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
	for darwin, want := range map[int]string{8: "10.4", 16: "10.12", 19: "10.15", 20: "11", 24: "15", 25: "26", 26: "27"} {
		got, err := ProductForDarwin(darwin)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, err := ProductForDarwin(27)
	require.Error(t, err, "a release the table does not carry")
	_, err = ReleaseForDarwin(16)
	require.Error(t, err)
}

func TestDescribeWordsDarwinAsMacOS(t *testing.T) {
	require.Equal(t, "macOS 26 (Tahoe) arm64", Describe(record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}))
	require.Equal(t, "macOS 15 (Sequoia)", Describe(record.Platform{OS: "darwin", Version: "24"}))
	require.Equal(t, "darwin 99 arm64", Describe(record.Platform{OS: "darwin", Version: "99", Architecture: "arm64"}), "an unknown release keeps the raw fields")
	require.Equal(t, "linux 6", Describe(record.Platform{OS: "linux", Version: "6"}))
}

// The table is the one place the release set is written. Everything that names
// it, or decides whether a platform is one of them, reads it from here.
func TestTheTableIsTheOnlyPlaceTheReleaseSetIsWritten(t *testing.T) {
	known := Known()
	require.NotEmpty(t, known)
	for i, release := range known {
		if i > 0 {
			require.Greater(t, release.Darwin, known[i-1].Darwin, "oldest first")
		}
		found, err := ReleaseForDarwin(release.Darwin)
		require.NoError(t, err)
		require.Equal(t, release, found)
		bySlug, err := ParseRelease(release.Slug)
		require.NoError(t, err)
		require.Equal(t, release, bySlug)
		byProduct, err := ParseRelease(release.Product)
		require.NoError(t, err)
		require.Equal(t, release, byProduct, "%s is selectable by its macOS version", release.Name)
	}
	// The refusal names every release rather than a list kept by hand.
	_, err := ParseRelease("nope")
	for _, release := range known {
		require.ErrorContains(t, err, release.Slug+" ("+release.Product+")")
	}
}

func TestGoldenGateIsMacOS27OnDarwin26(t *testing.T) {
	release, err := ReleaseForDarwin(26)
	require.NoError(t, err)
	require.Equal(t, Release{Darwin: 26, Product: "27", Name: "Golden Gate", Slug: "golden-gate"}, release)
	require.Equal(t, "macOS 27 (Golden Gate) arm64", Describe(record.Platform{OS: "darwin", Version: "26", Architecture: "arm64"}))
}
