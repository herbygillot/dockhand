package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStealthDistSubdirFollowsTheGuide(t *testing.T) {
	src := "name                croc\nversion             10.2.4\n\nchecksums           rmd160  aaa \\\n                    sha256  bbb \\\n                    size    7114508\n\nuse_configure       no\n"
	out, n, err := StealthDistSubdir([]byte(src))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, "name                croc\nversion             10.2.4\n\nchecksums           rmd160  aaa \\\n                    sha256  bbb \\\n                    size    7114508\ndist_subdir         ${name}/${version}_1\n\nuse_configure       no\n", string(out))

	// A second stealth update counts up.
	out, n, err = StealthDistSubdir(out)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Contains(t, string(out), "dist_subdir         ${name}/${version}_2\n")

	// No checksums line: at the end.
	out, _, err = StealthDistSubdir([]byte("name croc\n"))
	require.NoError(t, err)
	require.Equal(t, "name croc\ndist_subdir             ${name}/${version}_1\n", string(out))

	_, _, err = StealthDistSubdir([]byte("name croc\ndist_subdir go\n"))
	require.ErrorIs(t, err, ErrUnsupported)
	require.ErrorContains(t, err, "already set, to go")
}
