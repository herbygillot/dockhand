package command_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
)

// artifacts says, for each port and platform a branch's latest check
// passed, where its archive is, or why there's none, and a branch named
// twice is refused, where no command test drove it (the test analysis's
// step b).
func TestArtifactsSaysWhereEachPassedPortsArchiveIs(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	out, _, err := cmdtest.Dockhand(t, "artifacts")
	require.NoError(t, err)
	require.Contains(t, out, "jq")
	require.Contains(t, out, "its archive wasn't kept", "the command provider keeps none")
	_, _, err = cmdtest.Dockhand(t, "artifacts", "jq-update", "-b", "jq-update")
	require.ErrorContains(t, err, "name a branch once, as the argument or with -b, -p, or --pr")
	// A check is named as logs names one: artifacts check-781 read "no
	// tracked branch named check-781" (field testing, #68).
	named, _, err := cmdtest.Dockhand(t, "artifacts", "check-1")
	require.NoError(t, err)
	require.Equal(t, out, named)
	_, _, err = cmdtest.Dockhand(t, "artifacts", "check-1", "-b", "jq-update")
	require.ErrorContains(t, err, "name a check or a branch, not both")
	// A name that is no check or provider run is a branch's, resolved in
	// the one selection (the confirmation of #193's fixes, A4).
	branch, _, err := cmdtest.Dockhand(t, "artifacts", "jq-update")
	require.NoError(t, err)
	require.Equal(t, out, branch)
	_, _, err = cmdtest.Dockhand(t, "artifacts", "no-such-branch")
	require.ErrorContains(t, err, "no tracked branch named no-such-branch")
}
