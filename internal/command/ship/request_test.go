package ship_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/model"
)

// update reaches the resolver: its request is kept on the branch before
// its edit, each flag told given from left out, and the version it moved
// is concluded on its target (#195).
func TestUpdateReachesTheResolver(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	_, errs, err := cmdtest.Dockhand(t, "update", "jq", "--closes", "71234", "--keep-old-checksums")
	require.NoError(t, err, errs)
	request := cmdtest.KeptRequest(t, "jq-1.8.1")
	require.NotNil(t, request)
	require.Equal(t, "update", request.Origin)
	require.NotEmpty(t, request.ID)
	require.Equal(t, model.Set([]string{"71234"}), request.Actions.Closes)
	require.Equal(t, model.Set(true), request.Source.KeepOldChecksums)
	require.False(t, request.Shared.Given, "a flag left out stays left out")
	require.Len(t, request.Targets, 1)
	require.Equal(t, "jq", request.Targets[0].Port)
	outcome, ok := request.Targets[0].Outcome("version")
	require.True(t, ok)
	require.Equal(t, model.ActionDone, outcome.Result)
}

// ship reaches the resolver: a stage it stops at is kept, with the flags a
// later stage reads, and a ship again without --to stays there.
func TestShipReachesTheResolverAndKeepsItsStage(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	_, errs, err := cmdtest.Dockhand(t, "ship", "jq", "--to", "check", "--draft", "--yes")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "Kept for the submit: --draft.")
	request := cmdtest.KeptRequest(t, "jq-1.8.1")
	require.NotNil(t, request)
	require.Equal(t, "ship", request.Origin)
	require.Equal(t, model.Set("check"), request.To)
	require.Equal(t, model.Set(true), request.Publish.Draft)

	out, errs, err := cmdtest.Dockhand(t, "ship", "jq", "--yes")
	require.NoError(t, err, errs)
	require.Contains(t, errs, "stays at --to check, as kept")
	require.Contains(t, out, "jq-1.8.1  existing branch; goes on with 1.8.1, the release kept, to check, as kept")
	require.Empty(t, g.PRs, "a kept stage below submit stops it there")
}

// bump reaches the resolver: it is --to submit, given, over a stage a ship
// kept, and the publication its submission records takes the request,
// whose finishing ends it.
func TestBumpReachesTheResolverAndItsPublicationTakesTheRequest(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	_, errs, err := cmdtest.Dockhand(t, "ship", "jq", "--to", "check", "--yes")
	require.NoError(t, err, errs)
	require.NotNil(t, cmdtest.KeptRequest(t, "jq-1.8.1"))

	_, errs, err = cmdtest.Dockhand(t, "bump", "jq")
	require.NoError(t, err, errs)
	require.Len(t, g.PRs, 1, "bump goes to submit")
	require.Nil(t, cmdtest.KeptRequest(t, "jq-1.8.1"), "the finished publication ended the request it took")
}
