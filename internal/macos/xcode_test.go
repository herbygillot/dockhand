package macos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A release's Xcode image installs the Xcode it asks for, its archive
// found by version, Apple silicon's before the universal one: never a newer
// one in its place, nor a beta.
func TestSelectXcodeChoosesTheArchiveOfTheVersionAskedFor(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{
		"Xcode_15.4.xip",
		"Xcode_16.2.xip",
		"Xcode_26.3_Universal.xip",
		"Xcode_26.3_Apple_silicon.xip",
		"Xcode_26.6_Apple_silicon.xip",
		"Xcode_27.xip",
		"Xcode_27.1_beta.xip",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), nil, 0o600))
	}
	for _, test := range []struct {
		release Release
		version string
		name    string
	}{
		{Release{Darwin: 23, Name: "Sonoma", Tools: 16}, "15.4", "Xcode_15.4.xip"},
		{Release{Darwin: 24, Name: "Sequoia"}, "26.3", "Xcode_26.3_Apple_silicon.xip"},
		{Release{Darwin: 25, Name: "Tahoe"}, "26.6", "Xcode_26.6_Apple_silicon.xip"},
		{Release{Darwin: 27, Name: "Golden Gate"}, "27.0", "Xcode_27.xip"},
	} {
		path, version, err := SelectXcode(directory, test.release, test.version)
		require.NoError(t, err, test.release.Name)
		require.True(t, SameXcode(test.version, version))
		require.Equal(t, test.name, filepath.Base(path))
	}
	_, _, err := SelectXcode(directory, Release{Darwin: 24, Name: "Sequoia"}, "16.4")
	require.ErrorContains(t, err, "Sequoia's Xcode is 16.4, and ")
	require.ErrorContains(t, err, "has no archive of it (Xcode_16.4.xip); download Xcode 16.4 from https://developer.apple.com/download/all/")
	_, _, err = SelectXcode(directory, Release{Darwin: 27, Name: "Golden Gate"}, "27.1")
	require.ErrorContains(t, err, "no archive of it", "a beta is never chosen")
}

// An explicit archive is taken only as the version asked for, and a
// version is refused where it doesn't run, or where none is known.
func TestSelectXcodeChecksTheVersionAskedFor(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "Xcode_16.2.xip")
	require.NoError(t, os.WriteFile(archive, nil, 0o600))
	path, version, err := SelectXcode(archive, Release{Darwin: 23, Name: "Sonoma"}, "16.2")
	require.NoError(t, err)
	expected, err := filepath.EvalSymlinks(archive)
	require.NoError(t, err)
	require.Equal(t, expected, path)
	require.Equal(t, "16.2", version)
	_, _, err = SelectXcode(archive, Release{Darwin: 23, Name: "Sonoma"}, "15.4")
	require.ErrorContains(t, err, "Sonoma's Xcode is 15.4")
	_, _, err = SelectXcode(archive, Release{Darwin: 21, Name: "Monterey"}, "16.2")
	require.ErrorContains(t, err, "Xcode 16.2 doesn't run on Monterey; Xcode must be below 14.3")
	_, _, err = SelectXcode(archive, Release{Darwin: 30, Name: "Future"}, "")
	require.ErrorContains(t, err, "no Xcode is set for Future")
	_, _, err = SelectXcode(archive, Release{Darwin: 23, Name: "Sonoma"}, "16.2-beta")
	require.ErrorContains(t, err, "is not a version")
}

// Each release carries the Xcode MacPorts' arm64 buildbot for it runs,
// from the facts table, as it carries its tools generation.
func TestEachReleaseCarriesItsBuildersXcode(t *testing.T) {
	for _, release := range Known() {
		facts, ok := Table().BuilderXcode(release.Darwin)
		require.True(t, ok, release.Name)
		require.Equal(t, facts, release.Xcode, release.Name)
		require.NotEqual(t, "none", release.Xcode, release.Name)
	}
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
	for _, name := range []string{"Xcode_26.6_beta_2.xip", "Xcode_26.6_Release_Candidate.xip", "Xcode.app", "notes.txt",
		"Xcode-27.1.0-beta.2+27B5024e.xip", "Xcode-26.6.0+17F113.xip.aria2"} {
		_, ok := parseXcodeArchive(name)
		require.False(t, ok, name)
	}
}

// xcodes names what it downloads by its full version and build, and setup
// finds it as it finds Apple's names; a missing Xcode says where it was
// looked for, and whether an archive can be downloaded there.
func TestAnXcodesDownloadIsFound(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "Xcode-15.4.0+15F31d.xip"), nil, 0o600))
	sonoma := Release{Darwin: 23, Name: "Sonoma"}
	path, version, err := SelectXcode(directory, sonoma, "15.4")
	require.NoError(t, err)
	require.Equal(t, "Xcode-15.4.0+15F31d.xip", filepath.Base(path))
	require.Equal(t, "15.4.0", version)

	_, _, err = SelectXcode(directory, Release{Darwin: 24, Name: "Sequoia"}, "16.4")
	var missing *MissingXcode
	require.ErrorAs(t, err, &missing)
	require.Equal(t, "16.4", missing.Version)
	require.NotEmpty(t, missing.Folder, "a folder can take a download")
	_, _, err = SelectXcode(path, sonoma, "16.2")
	require.ErrorAs(t, err, &missing)
	require.Empty(t, missing.Folder, "a single archive can't")
}

// Only Apple's signature passes.
func TestAnArchiveNotSignedByAppleIsRefused(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "Xcode_26.6.xip")
	require.NoError(t, os.WriteFile(archive, []byte("not an archive"), 0o600))
	require.ErrorContains(t, CheckXcodeSignature(t.Context(), archive), "isn't signed by Apple")
}
