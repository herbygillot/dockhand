package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const croc = "name                croc\nversion             10.2.4\n\nchecksums           rmd160  aaa \\\n                    sha256  bbb \\\n                    size    7114508\n\nuse_configure       no\n"

func TestStealthDistSubdirFollowsTheRevisionItBumps(t *testing.T) {
	out, form, err := StealthDistSubdir([]byte(croc), true)
	require.NoError(t, err)
	require.Equal(t, DistSubdir{ByRevision: true}, form)
	require.Equal(t, "name                croc\nversion             10.2.4\n\nchecksums           rmd160  aaa \\\n                    sha256  bbb \\\n                    size    7114508\ndist_subdir         ${name}/${version}_${revision}\n\nuse_configure       no\n", string(out))

	// The next stealth update's revision bump moves it again.
	again, form, err := StealthDistSubdir(out, true)
	require.NoError(t, err)
	require.Equal(t, DistSubdir{ByRevision: true}, form)
	require.Equal(t, string(out), string(again))

	// Without the bump, it would name the old archive's directory.
	_, _, err = StealthDistSubdir(out, false)
	require.ErrorIs(t, err, ErrUnsupported)
	require.ErrorContains(t, err, "without a revision bump")
}

func TestStealthDistSubdirCountsWithoutARevisionBump(t *testing.T) {
	out, form, err := StealthDistSubdir([]byte(croc), false)
	require.NoError(t, err)
	require.Equal(t, DistSubdir{Counter: 1}, form)
	require.Contains(t, string(out), "size    7114508\ndist_subdir         ${name}/${version}_1\n")

	// A number already there keeps counting, bumped or not: _${revision}
	// with revision 1 would be the _1 the old archive is in.
	out, form, err = StealthDistSubdir(out, true)
	require.NoError(t, err)
	require.Equal(t, DistSubdir{Counter: 2}, form)
	require.Contains(t, string(out), "dist_subdir         ${name}/${version}_2\n")

	out, _, err = StealthDistSubdir([]byte("name croc\n"), false)
	require.NoError(t, err)
	require.Equal(t, "name croc\ndist_subdir             ${name}/${version}_1\n", string(out))

	_, _, err = StealthDistSubdir([]byte("name croc\ndist_subdir go\n"), true)
	require.ErrorIs(t, err, ErrUnsupported)
	require.ErrorContains(t, err, "already set, to go")
}

func TestANewVersionDropsTheStealthDistSubdir(t *testing.T) {
	for _, value := range []string{"${name}/${version}_${revision}", "${name}/${version}_3"} {
		out, removed, err := RemoveStealthDistSubdir([]byte("name croc\nversion 10.3.0\ndist_subdir         " + value + "\nuse_configure no\n"))
		require.NoError(t, err)
		require.True(t, removed, value)
		require.Equal(t, "name croc\nversion 10.3.0\nuse_configure no\n", string(out))
	}
	out, removed, err := RemoveStealthDistSubdir([]byte("name croc\ndist_subdir go\n"))
	require.NoError(t, err)
	require.False(t, removed)
	require.Equal(t, "name croc\ndist_subdir go\n", string(out))
}
