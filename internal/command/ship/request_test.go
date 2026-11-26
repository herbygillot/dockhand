package ship_test

import (
	"encoding/json"
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

// A shaping flag changed after the version's edit is refused by name and
// stops the branch, attended or not, until it is given as it was (#195,
// AW).
func TestAShapingFlagChangedAfterTheEditStopsShip(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	_, errs, err := cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err, errs)
	for _, args := range [][]string{{"ship", "jq", "--keep-http", "--yes"}, {"bump", "jq", "--keep-http"}} {
		out, errs, err := cmdtest.Dockhand(t, args...)
		require.Error(t, err, "%v", args)
		require.Contains(t, out+errs+err.Error(), "--keep-http changed after jq's edit to 1.8.1", "%v", args)
		require.Empty(t, g.PRs, "%v", args)
	}
	_, errs, err = cmdtest.Dockhand(t, "ship", "jq", "--keep-http=false", "--yes")
	require.NoError(t, err, errs)
	require.Len(t, g.PRs, 1, "given as it was, the work goes on")
}

// ship --plan's JSON carries what its prose says: each target, how far it
// goes and whether that was kept, and what isn't resolved now (#195).
func TestShipsPlanJSONCarriesTheValue(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	out, errs, err := cmdtest.Dockhand(t, "ship", "jq", "--plan", "--json")
	require.NoError(t, err, errs)
	var result struct {
		Result struct {
			Plan []struct {
				Target, Stage string
				StageKept     bool `json:"stage_kept"`
				Unresolved    []struct{ What, When string }
			}
		}
	}
	require.NoError(t, json.Unmarshal([]byte(out), &result), out)
	require.Len(t, result.Result.Plan, 1, out)
	row := result.Result.Plan[0]
	require.Equal(t, "jq", row.Target)
	require.Equal(t, "submit", row.Stage)
	require.False(t, row.StageKept)
	require.Contains(t, row.Unresolved, struct{ What, When string }{"the destination", "decided at submit"})
}

// check and submit read what a ship kept, where they aren't given it, and
// say which; neither changes the request (#195).
func TestCheckAndSubmitReadWhatShipKept(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	_, errs, err := cmdtest.Dockhand(t, "ship", "jq", "--to", "commit", "--on", "command", "--tests", "skip", "--draft", "--type", "bugfix", "--yes")
	require.NoError(t, err, errs)
	kept := cmdtest.KeptRequest(t, "jq-1.8.1")
	require.NotNil(t, kept)

	out, errs, err := cmdtest.Dockhand(t, "check", "-b", "jq-1.8.1", "--plan")
	require.NoError(t, err, errs)
	require.Contains(t, out, "Taken from what the branch's request kept: --on command, --tests skip")
	out, _, _ = cmdtest.Dockhand(t, "check", "-b", "jq-1.8.1", "--plan", "--tests", "declared")
	require.Contains(t, out, "Taken from what the branch's request kept: --on command\n", "a flag given is this check's own")

	out, _, _ = cmdtest.Dockhand(t, "submit", "-b", "jq-1.8.1", "--plan")
	require.Contains(t, out, "Taken from what the branch's request kept: --draft, --type bugfix")
	after := cmdtest.KeptRequest(t, "jq-1.8.1")
	require.Equal(t, kept.ID, after.ID, "neither changed the request")
	require.Equal(t, kept.Check, after.Check)
	require.Equal(t, kept.Publish, after.Publish)
}

// A note given to ship and kept on the branch is said as kept on the
// branch: it read "kept from an earlier submit", though no submit had run
// (field testing's #195 batch).
func TestANoteShipKeptIsSaidAsKeptOnTheBranch(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	_, errs, err := cmdtest.Dockhand(t, "ship", "jq", "--to", "commit", "--note", "Tested on a fresh prefix.", "--yes")
	require.NoError(t, err, errs)
	out, _, _ := cmdtest.Dockhand(t, "submit", "-b", "jq-1.8.1", "--plan")
	require.Contains(t, out, "Tested on a fresh prefix.")
	require.Contains(t, out, "kept on the branch since it was given, across rewritten commits; --no-note clears it")
	require.NotContains(t, out, "earlier submit")
}

// --no-check given to submit replaces the --draft ship kept, the two being
// one choice, and says so: the kept --draft was dropped unsaid, and the
// pull request opened ready (field testing's #195 batch).
func TestNoCheckGivenSaysItReplacesTheKeptDraft(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	_, errs, err := cmdtest.Dockhand(t, "ship", "jq", "--to", "commit", "--draft", "--type", "bugfix", "--yes")
	require.NoError(t, err, errs)
	out, errs, err := cmdtest.Dockhand(t, "submit", "-b", "jq-1.8.1", "--no-check", "--plan")
	require.NoError(t, err, errs)
	require.Contains(t, out, "Taken from what the branch's request kept: --type bugfix\n")
	require.Contains(t, out, "Given over what the branch's request kept: --no-check replaces the kept --draft\n")
	// Said by the submission itself too, not only its plan.
	out, errs, err = cmdtest.Dockhand(t, "submit", "-b", "jq-1.8.1", "--no-check", "--yes")
	require.NoError(t, err, errs)
	require.Contains(t, out, "Given over what the branch's request kept: --no-check replaces the kept --draft\n")
	require.Len(t, g.PRs, 1)
}
