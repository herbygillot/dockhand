package provision

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/platform"
)

func xipDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644))
	}
	return dir
}

func release(t *testing.T, name string) platform.Release {
	t.Helper()
	r, err := platform.Parse(name)
	require.NoError(t, err)
	return r
}

func TestPickXcodeHonorsEachReleasesBound(t *testing.T) {
	dir := xipDir(t, "Xcode_14.2.xip", "Xcode_15.2.xip", "Xcode_16.2.xip", "Xcode_26.2.xip")
	for want, rel := range map[string]string{
		"14.2": "monterey",
		"15.2": "ventura",
		"16.2": "sonoma",
		"26.2": "sequoia", // 26.2 < the 26.4 bound
	} {
		_, v, err := PickXcode(dir, release(t, rel))
		require.NoError(t, err, rel)
		assert.Equal(t, want, v, rel)
	}
	// The newest release has no bound and takes the newest archive.
	_, v, err := PickXcode(dir, release(t, "tahoe"))
	require.NoError(t, err)
	assert.Equal(t, "26.2", v)
}

func TestPickXcodeRefusesWhenNothingFits(t *testing.T) {
	dir := xipDir(t, "Xcode_26.2.xip")
	_, _, err := PickXcode(dir, release(t, "monterey"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires Xcode below 14.3")
	assert.Contains(t, err.Error(), "26.2")
}

func TestPickXcodeAcceptsOneArchiveButStillJudgesIt(t *testing.T) {
	dir := xipDir(t, "Xcode_16.2.xip")
	one := filepath.Join(dir, "Xcode_16.2.xip")
	path, v, err := PickXcode(one, release(t, "sonoma"))
	require.NoError(t, err)
	assert.Equal(t, one, path)
	assert.Equal(t, "16.2", v)
	_, _, err = PickXcode(one, release(t, "monterey"))
	require.Error(t, err, "a directly named archive is held to the bound, not trusted")
}

func TestPickXcodeSkipsBetasAndStrangers(t *testing.T) {
	dir := xipDir(t, "Xcode_26.1_beta_2.xip", "Xcode_16.2_Release_Candidate.xip",
		"notes.txt", "Xcode_16.2.xip")
	_, v, err := PickXcode(dir, release(t, "tahoe"))
	require.NoError(t, err)
	assert.Equal(t, "16.2", v, "only the release archive counts")
}

func TestPickXcodeNamesAnEmptyDirectory(t *testing.T) {
	_, _, err := PickXcode(xipDir(t), release(t, "tahoe"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Xcode_<version>.xip archives")
}

func TestXipVersion(t *testing.T) {
	for name, want := range map[string]string{
		"Xcode_16.2.xip":   "16.2",
		"Xcode_26.0.1.xip": "26.0.1",
	} {
		v, ok := xipVersion(name)
		assert.True(t, ok, name)
		assert.Equal(t, want, v)
	}
	for _, name := range []string{"Xcode_26.1_beta.xip", "Xcode.xip", "Xcode_.xip", "clang.xip"} {
		_, ok := xipVersion(name)
		assert.False(t, ok, name)
	}
}
