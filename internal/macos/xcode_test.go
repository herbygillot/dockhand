package macos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectXcodeChoosesNewestCompatibleArchive(t *testing.T) {
	directory := t.TempDir()
	archives := []string{
		"Xcode_14.2.xip",
		"Xcode_15.2.xip",
		"Xcode_16.2.xip",
		"Xcode_26.3_Universal.xip",
		"Xcode_26.3_Apple_silicon.xip",
		"Xcode_26.6_Apple_silicon.xip",
		"Xcode_27_beta.xip",
	}
	for _, name := range archives {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), nil, 0o600))
	}
	tests := []struct {
		darwin  int
		version string
		name    string
	}{
		{21, "14.2", "Xcode_14.2.xip"},
		{22, "15.2", "Xcode_15.2.xip"},
		{23, "16.2", "Xcode_16.2.xip"},
		{24, "26.3", "Xcode_26.3_Apple_silicon.xip"},
		{25, "26.6", "Xcode_26.6_Apple_silicon.xip"},
	}
	for _, test := range tests {
		path, version, err := SelectXcode(directory, Release{Darwin: test.darwin, Name: "fixture"})
		require.NoError(t, err)
		require.Equal(t, test.version, version)
		require.Equal(t, test.name, filepath.Base(path))
	}
}

func TestSelectXcodeChecksAnExplicitArchiveForCompatibility(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "Xcode_16.2.xip")
	require.NoError(t, os.WriteFile(archive, nil, 0o600))
	path, version, err := SelectXcode(archive, Release{Darwin: 23, Name: "Sonoma"})
	require.NoError(t, err)
	expected, err := filepath.EvalSymlinks(archive)
	require.NoError(t, err)
	require.Equal(t, expected, path)
	require.Equal(t, "16.2", version)
	_, _, err = SelectXcode(archive, Release{Darwin: 21, Name: "Monterey"})
	require.ErrorContains(t, err, "Xcode must be below 14.3")
}

// A release's Xcode is no older than its own tools generation, which has
// its SDK: Golden Gate, generation 27, takes Xcode 27 over 26.6, and is
// refused rather than given 26.6 when there is no 27. A beta never counts.
func TestSelectXcodeIsNoOlderThanTheReleasesGeneration(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"Xcode_26.3_Apple_silicon.xip", "Xcode_26.6_Apple_silicon.xip", "Xcode_27.1_beta.xip", "Xcode_27.xip"} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), nil, 0o600))
	}
	goldenGate := Release{Darwin: 27, Name: "Golden Gate", Tools: 27}
	path, version, err := SelectXcode(directory, goldenGate)
	require.NoError(t, err)
	require.Equal(t, "27", version)
	require.Equal(t, "Xcode_27.xip", filepath.Base(path))

	require.NoError(t, os.Remove(filepath.Join(directory, "Xcode_27.xip")))
	_, _, err = SelectXcode(directory, goldenGate)
	require.ErrorContains(t, err, "no Xcode archive fits Golden Gate; Xcode must be at least 27")
	_, _, err = SelectXcode(filepath.Join(directory, "Xcode_26.6_Apple_silicon.xip"), goldenGate)
	require.ErrorContains(t, err, "Xcode must be at least 27")

	path, version, err = SelectXcode(directory, Release{Darwin: 24, Name: "Sequoia", Tools: 16})
	require.NoError(t, err, "a newer generation fits, below the release's bound")
	require.Equal(t, "26.3", version)
	require.Equal(t, "Xcode_26.3_Apple_silicon.xip", filepath.Base(path))
}

// An archive's version is the installed Xcode's, however it is spelled:
// Xcode_27.xip installs the Xcode that calls itself 27.0.
func TestSameXcodeComparesNumbers(t *testing.T) {
	require.True(t, SameXcode("27", "27.0"))
	require.True(t, SameXcode("26.6", "26.6.0"))
	require.False(t, SameXcode("26.6", "26.6.1"))
	require.False(t, SameXcode("27", "26.6"))
	require.False(t, SameXcode("27", "27_beta"), "a version that isn't numeric matches only itself")
	require.True(t, SameXcode("", ""))
}

func TestParseXcodeArchiveRejectsPrereleasesAndOtherFiles(t *testing.T) {
	for _, name := range []string{"Xcode_26.6_beta_2.xip", "Xcode_26.6_Release_Candidate.xip", "Xcode.app", "notes.txt"} {
		_, ok := parseXcodeArchive(name)
		require.False(t, ok, name)
	}
}
