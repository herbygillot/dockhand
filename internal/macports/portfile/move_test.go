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
	require.EqualError(t, err, "Dockhand can't rewrite this github.setup repository because it uses Tcl substitution. Set it to GitoxideLabs/gitoxide by hand")
	require.Equal(t, "github.setup", SetupCommand([]byte(src), "Byron/gitoxide"))
}

// A go.setup's module path on github.com is moved as github.setup's words
// are, any package path after the repository kept: oasdiff's was left as
// it was, and held again with the same advice (#167).
func TestMoveGitHubMovesAGoSetup(t *testing.T) {
	src := "PortGroup golang 1.0\ngo.setup github.com/Tufin/oasdiff 1.10.0 v\nsubport oasdiff-cli {\n    go.setup github.com/Tufin/oasdiff/cmd 1.10.0 v\n}\n"
	require.Equal(t, "go.setup", SetupCommand([]byte(src), "tufin/oasdiff"))
	moved, n, err := MoveGitHub([]byte(src), "Tufin/oasdiff", "oasdiff/oasdiff")
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, "PortGroup golang 1.0\ngo.setup github.com/oasdiff/oasdiff 1.10.0 v\nsubport oasdiff-cli {\n    go.setup github.com/oasdiff/oasdiff/cmd 1.10.0 v\n}\n", string(moved))

	_, _, err = MoveGitHub([]byte("go.setup github.com/${author}/oasdiff 1.10.0 v\n"), "Tufin/oasdiff", "oasdiff/oasdiff")
	require.EqualError(t, err, "Dockhand can't rewrite this go.setup module path because it uses Tcl substitution. Set it to github.com/oasdiff/oasdiff by hand")
	require.Equal(t, "github.setup", SetupCommand([]byte("go.setup golang.org/x/tools 0.1 v\n"), "Tufin/oasdiff"))
}
