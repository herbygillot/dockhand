package engine

import (
	"bytes"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// The guards that keep tidy from rewriting history it shouldn't (the test
// plan's step 2, items 12 and 13).

// A saved plan whose branch's base moved since is refused as stale, as
// one whose branch moved already is; and a saved commit with no subject,
// or no author with a name and an email, holds the plan.
func TestASavedPlanIsHeldOrRefusedForWhatItLacks(t *testing.T) {
	t.Parallel()
	e, plan := threeChanges(t)
	regrouped, err := plan.Regroup("1+2 3", "")
	require.NoError(t, err)
	data, err := regrouped.Save()
	require.NoError(t, err)
	var saved savedTidyPlan
	_, err = toml.Decode(string(data), &saved)
	require.NoError(t, err)
	encode := func(plan savedTidyPlan) []byte {
		var b bytes.Buffer
		require.NoError(t, toml.NewEncoder(&b).Encode(plan))
		return b.Bytes()
	}

	moved := saved
	moved.Base = saved.Head
	_, err = e.LoadTidyPlan(t.Context(), encode(moved))
	require.ErrorIs(t, err, ErrStalePlan)
	require.ErrorContains(t, err, "its base moved from "+short(model.ObjectID(saved.Head))+" to "+short(plan.Branch.Base))

	unsigned := saved
	unsigned.Commits = append([]savedCommit(nil), saved.Commits...)
	unsigned.Commits[0].Message = "\n"
	unsigned.Commits[1].Author.Email = "no address"
	loaded, err := e.LoadTidyPlan(t.Context(), encode(unsigned))
	require.NoError(t, err)
	require.Equal(t, []string{"commit 1 needs a subject", "commit 2 needs an author with a name and an email"}, loaded.Blocking())
	_, err = e.ApplyTidy(t.Context(), loaded)
	require.Error(t, err, "a held plan isn't applied")
}

// A squash of several commits, by several people, is held for its message
// and its attribution; one commit's would take its subject.
func TestASquashIsHeldForItsMessage(t *testing.T) {
	t.Parallel()
	e, plan := threeChanges(t)
	squash, err := e.PlanTidy(t.Context(), TidyRequest{Branch: plan.Branch, Squash: true})
	require.NoError(t, err)
	require.Equal(t, []string{`a squash needs its message: --message "port: what changed"`, "the squash combines commits by Ada <ada@example.org> and Bo <bo@example.org>; choose the attribution with --author"}, squash.Blocking())
	squash, err = e.PlanTidy(t.Context(), TidyRequest{Branch: plan.Branch, Squash: true, Message: "libharbor: update to 3", Author: "Ada <ada@example.org>"})
	require.NoError(t, err)
	require.Empty(t, squash.Blocking())
}
