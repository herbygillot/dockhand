package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The repositories a Portfile's github.setup lines, and go.setup lines
// naming a github.com package, name in literal words are read from its
// text, a subport's too; one named by substitution isn't.
func TestGitHubSetups(t *testing.T) {
	src := "PortSystem 1.0\nPortGroup github 1.0\ngithub.setup Byron gitoxide 0.45.0 v\nsubport gix-devel {\n    github.setup GitoxideLabs gitoxide 0.46.0 v\n}\nsubport other {\n    github.setup ${author} other 1.0\n}\n"
	require.Equal(t, []string{"Byron/gitoxide", "GitoxideLabs/gitoxide"}, GitHubSetups([]byte(src)))
	require.Equal(t, []string{"nektos/act", "spf13/cobra"}, GitHubSetups([]byte("PortGroup golang 1.0\ngo.setup github.com/nektos/act 0.2.92 v\nsubport cobra {\n    go.setup github.com/spf13/cobra/cmd 1.0\n}\ngo.setup golang.org/x/tools 0.1\ngo.setup github.com/${author}/x 1.0\n")))
	require.Empty(t, GitHubSetups([]byte("PortSystem 1.0\nversion 1.0\n")))
	require.Empty(t, GitHubSetups([]byte("github.setup {unclosed\n")))
}
