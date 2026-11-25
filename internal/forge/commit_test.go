package forge

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A commit as a forge names it is forty lowercase hex digits whole, and
// seven to thirty-nine abbreviated: narrower than what Git accepts as an
// object ID, which takes upper case and a SHA-256's sixty-four.
func TestACommitAsAForgeNamesIt(t *testing.T) {
	t.Parallel()
	whole := strings.Repeat("a1", 20)
	require.True(t, FullCommit(whole))
	require.False(t, FullCommit(strings.ToUpper(whole)), "upper case isn't how a forge names it")
	require.False(t, FullCommit(strings.Repeat("a", 64)), "a SHA-256 object ID isn't a forge's commit")
	require.False(t, FullCommit(whole[:39]))
	require.True(t, AbbreviatedCommit(whole[:7]))
	require.True(t, AbbreviatedCommit(whole[:39]))
	require.False(t, AbbreviatedCommit(whole), "a whole commit isn't abbreviated")
	require.False(t, AbbreviatedCommit(whole[:6]))
	require.False(t, AbbreviatedCommit("1.2.3"))
}
