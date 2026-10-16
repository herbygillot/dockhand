package command

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// shipAnswering runs ship on a terminal that answers.
func shipAnswering(t *testing.T, answers string, args ...string) (string, string, error) {
	t.Helper()
	var out, errs bytes.Buffer
	err := Run(t.Context(), append([]string{"ship"}, args...), Streams{In: strings.NewReader(answers), Out: &out, Err: &errs, interactive: true})
	return out.String(), errs.String(), err
}

// ship carries a port from master to its pull request, showing its plan and
// asking once; run again, it resumes the branch, here already submitted
// (the command-line UX second look's §6).
func TestShipCarriesAPortToItsPullRequest(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)
	g.Others = nil

	out, errs, err := shipAnswering(t, "n\n", "jq")
	require.NoError(t, err, errs)
	require.Contains(t, out, "  jq  a version update from master, to submit\n")
	require.Contains(t, errs, "? Ship it? [y/N] ")
	require.Contains(t, out, "Nothing was shipped.")
	require.Empty(t, g.PRs)

	out, errs, err = shipAnswering(t, "y\nn\nn\n", "jq")
	require.NoError(t, err, errs)
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1")
	require.Contains(t, out, "checking commit ")
	require.Contains(t, out, "Opened #34901")
	require.Len(t, g.PRs, 1)

	out, _, err = dockhand(t, "ship", "jq", "--to", "check")
	require.NoError(t, err)
	require.Contains(t, out, "  jq-1.8.1  its branch, where it stands, to check\n", "resumed, not started again")
	require.Contains(t, out, "passed for this commit already; nothing to check")
}

// --to commit stops at the commit, and --to names a stage.
func TestShipStopsWhereToSays(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	g := withGitHub(t, w)
	g.Others = nil

	_, _, err := dockhand(t, "ship", "jq", "--to", "review")
	require.EqualError(t, err, "--to review: commit, check, or submit")
	out, errs, err := dockhand(t, "ship", "jq", "--to", "commit")
	require.NoError(t, err, errs)
	require.Contains(t, out, "jq: 1.7.1 → 1.8.1")
	require.NotContains(t, out, "checking commit ")
	require.Empty(t, g.PRs)
	_, _, err = dockhand(t, "ship", "jq", "-b", "jq-1.8.1")
	require.EqualError(t, err, "name ports or a branch, not both")
}
