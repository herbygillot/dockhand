package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A github.setup naming a repository GitHub now answers by another name
// is pointed at it, word for word, where its words are literals (update
// --follow-moves).
func TestMoveGitHub(t *testing.T) {
	src := "PortSystem 1.0\nPortGroup github 1.0\ngithub.setup Byron gitoxide 0.45.0 v\nsubport gix-devel {\n    github.setup Byron gitoxide 0.46.0 v\n}\n"
	moved, n, err := MoveGitHub([]byte(src), "Byron/gitoxide", "GitoxideLabs/gitoxide")
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, "PortSystem 1.0\nPortGroup github 1.0\ngithub.setup GitoxideLabs gitoxide 0.45.0 v\nsubport gix-devel {\n    github.setup GitoxideLabs gitoxide 0.46.0 v\n}\n", string(moved))

	same, n, err := MoveGitHub([]byte(src), "someone/else", "GitoxideLabs/gitoxide")
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, src, string(same))

	_, _, err = MoveGitHub([]byte("github.setup ${author} gitoxide 0.45.0 v\n"), "Byron/gitoxide", "GitoxideLabs/gitoxide")
	require.ErrorContains(t, err, "names its repository by substitution")
}
