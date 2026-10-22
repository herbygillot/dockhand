package explain_test

import (
	"path/filepath"
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

// explain says why a check ended as it did, and where a branch stands
// (the command UX review's §7).
func TestExplainSaysWhyACheckAndABranch(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)

	out, _, err := cmdtest.Dockhand(t, "explain", "-b", "jq-update")
	require.NoError(t, err)
	require.Equal(t, "jq-update is under way: nothing has checked it yet; nothing needs you yet\nNext: dockhand check --branch jq-update\n", out)

	cmdtest.WithScript(t, w, "failed")
	_, _, err = cmdtest.Dockhand(t, "check")
	require.Error(t, err)
	out, _, err = cmdtest.Dockhand(t, "explain", "check-1")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update's check, of snapshot 1: failed")
	require.Contains(t, out, "\n  ✗ jq on command: failed at install")
	require.Contains(t, out, "Next: dockhand logs check-1 jq\n")
	out, _, err = cmdtest.Dockhand(t, "explain", "-b", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update needs you\n  ✗ jq-update  ")
	require.Contains(t, out, "Next: dockhand logs check-1 jq\n")

	cmdtest.WithScript(t, w, "passed")
	_, _, err = cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	out, _, err = cmdtest.Dockhand(t, "explain", "check-2")
	require.NoError(t, err)
	require.Equal(t, "jq-update's check, of snapshot 1: passed on command\n", out)

	_, _, err = cmdtest.Dockhand(t, "explain", "check-2", "-b", "jq-update")
	require.EqualError(t, err, "name one thing to explain: check-2, or a branch with -b, -p, or --pr")
	_, _, err = cmdtest.Dockhand(t, "explain", "check-9")
	require.Error(t, err)
}
