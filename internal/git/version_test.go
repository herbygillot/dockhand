package git

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A Git's version is read from what git version says, whoever built it,
// and compared with the oldest dockhand works with.
func TestGitVersionsAreReadAndCompared(t *testing.T) {
	for reported, want := range map[string]Version{
		"git version 2.54.0 (Apple Git-157)": {Major: 2, Minor: 54, Patch: 0},
		"git version 2.39.5 (Apple Git-154)": {Major: 2, Minor: 39, Patch: 5},
		"git version 2.40.0.rc1":             {Major: 2, Minor: 40, Patch: 0},
		"git version 2.45.2.windows.1":       {Major: 2, Minor: 45, Patch: 2},
		"git version 3.0":                    {Major: 3, Minor: 0},
	} {
		got, err := parseVersion(reported)
		require.NoError(t, err, reported)
		want.Reported = reported
		require.Equal(t, want, got, reported)
	}
	_, err := parseVersion("hub version 2.14.2")
	require.Error(t, err)

	old, _ := parseVersion("git version 2.39.5 (Apple Git-154)")
	require.False(t, old.AtLeast(MinimumVersion))
	for _, reported := range []string{"git version 2.40.0", "git version 2.54.0", "git version 3.0"} {
		current, _ := parseVersion(reported)
		require.True(t, current.AtLeast(MinimumVersion), reported)
	}
	require.Equal(t, "2.40", MinimumVersion.String())
	require.Equal(t, "2.39.5", old.String())
}
