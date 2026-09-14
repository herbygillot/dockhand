package provision

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/verify/tart"
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
		path, version, err := selectXcode(directory, tart.MacOSRelease{Darwin: test.darwin, Name: "fixture"})
		require.NoError(t, err)
		require.Equal(t, test.version, version)
		require.Equal(t, test.name, filepath.Base(path))
	}
}

func TestSelectXcodeChecksAnExplicitArchiveForCompatibility(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "Xcode_16.2.xip")
	require.NoError(t, os.WriteFile(archive, nil, 0o600))
	path, version, err := selectXcode(archive, tart.MacOSRelease{Darwin: 23, Name: "Sonoma"})
	require.NoError(t, err)
	expected, err := filepath.EvalSymlinks(archive)
	require.NoError(t, err)
	require.Equal(t, expected, path)
	require.Equal(t, "16.2", version)
	_, _, err = selectXcode(archive, tart.MacOSRelease{Darwin: 21, Name: "Monterey"})
	require.ErrorContains(t, err, "Xcode must be below 14.3")
}

func TestParseXcodeArchiveRejectsPrereleasesAndOtherFiles(t *testing.T) {
	for _, name := range []string{"Xcode_26.6_beta_2.xip", "Xcode_26.6_Release_Candidate.xip", "Xcode.app", "notes.txt"} {
		_, ok := parseXcodeArchive(name)
		require.False(t, ok, name)
	}
}
