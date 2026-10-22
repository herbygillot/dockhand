package explain_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/stretchr/testify/require"
)

func TestExplainQuotesTheSource(t *testing.T) {
	t.Parallel()
	out, _, err := cmdtest.Dockhand(t, "explain")
	require.NoError(t, err)
	require.Contains(t, out, "follow-up              A commit that only corrects an earlier one in the branch,")
	require.Contains(t, out, "revision-after-update  When a port's version changes, its revision goes back to 0:")

	out, _, err = cmdtest.Dockhand(t, "explain", "follow-up")
	require.NoError(t, err)
	require.Contains(t, out, "follow-up\n\nA commit that only corrects")
	require.Contains(t, out, "The MacPorts Guide, Contributing to MacPorts\nhttps://guide.macports.org/#project.github\n  “Be sure to rebase your changes so as to minimize the number of commits.")
	require.Contains(t, out, "  with one commit per logical change.)”\n")

	out, _, err = cmdtest.Dockhand(t, "explain", "subject-length")
	require.NoError(t, err)
	require.Contains(t, out, "https://trac.macports.org/wiki/CommitMessages\n  (not quoted here; the link has its text)\n")

	_, _, err = cmdtest.Dockhand(t, "explain", "nope")
	require.ErrorContains(t, err, `no rule is called "nope"; dockhand explain lists them`)
}
