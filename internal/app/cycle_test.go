package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

// mintedChange is a --no-verify bump through the Change operation: the
// record, the branch and (now) the note, with no provider anywhere near
// it. It is the fixture every pass-level test below runs over, because a
// pass reads the store and the store has to have been written by
// something.
func mintedChange(t *testing.T, repo *git.Repo, st *statestore.Store, l *ledger.Ledger) Result {
	t.Helper()
	op := Change{Repo: repo, State: st, Ledger: l, Me: me(), Now: now, Progress: &sink{}}
	res, err := op.Run(t.Context(), ChangeRequest{
		Prepared: preparedBump(t, repo, "jq", "1.8"),
		Slug:     "jq-1.8",
		Delivery: Branch,
	})
	require.NoError(t, err)
	require.Equal(t, Minted, res.Did)
	return res
}

// A FAILED PUBLICATION IS NOT A PUBLICATION. The publish stage keeps
// publish.Apply's Outcome on the error path — Completed is how a caller
// tells "pushed, no pull request" from "never left the machine" — so the
// count a report prints cannot be the length of that slice.
func TestPassPublicationsCountsOnlyWhatReachedReviewers(t *testing.T) {
	p := Pass{Published: []publish.Outcome{
		{}, // Apply refused before any I/O at all: an ErrStale over a zero Outcome
		{Completed: []record.StepKind{record.PushBranch}}, // on the fork, in front of nobody
		{Completed: []record.StepKind{record.PushBranch, record.OpenPR}, Number: 7, URL: "https://example.invalid/pr/7"},
		{Completed: []record.StepKind{record.RefreshPR}, Number: 4, URL: "https://example.invalid/pr/4"},
	}}
	got := p.Publications()
	require.Len(t, got, 2, "an opening and a refresh; a bare push and a refusal are neither")
	assert.Equal(t, 7, got[0].Number)
	assert.Equal(t, 4, got[1].Number)
	assert.Empty(t, Pass{}.Publications(), "a pass that published nothing publishes nothing")
}

// A FORGE THAT COULD NOT BE ASKED IS NOT A FORGE THAT SAID "STILL OPEN".
// publish.Standing refuses both stale facts and a failed lookup, and the
// close stage used to collapse that refusal and the still-open answer
// into one `continue`: ten open publications on a host whose `gh` is
// gone were skipped every tick, the pass printed "0 retired · 0 refused"
// and Attention() was false, so `dispatch --once` exited 0.
func TestCloseStageRefusesWhenTheForgeCouldNotBeAsked(t *testing.T) {
	ctx := t.Context()
	repo, st := fixture(t)
	l := ledger.Open(repo)
	res := mintedChange(t, repo, st, l)
	id := res.Ref.ID()

	// An open publication of that change, with no step taken yet: the row
	// a promotion leaves behind and the pass is meant to retire.
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		tx.PutPublication(record.Publication{
			ID: "pub-01", Change: id, By: record.Human, Outcome: record.Open, Content: res.Ref.Content(),
		})
		return nil
	}))

	broken := errors.New("gh: command not found")
	c := Cycle{
		Repo: repo, State: st, Ledger: l, Me: me(), Now: now, Progress: &sink{},
		Grants: Grants{Invoker: record.Human},
		Env: publish.Env{Repo: repo, State: st, Forge: func(context.Context, ...string) (string, error) {
			return "", broken
		}},
	}
	p, err := c.Run(ctx, CycleRequest{Forge: publish.ForgeRefresh})
	require.NoError(t, err, "a forge that could not be asked is a refusal row, never a stop")

	require.Len(t, p.Refusals, 1, "rule 7: could not ask is not nothing to retire")
	assert.Equal(t, id, p.Refusals[0].Change)
	require.Error(t, p.Refusals[0].Err)
	assert.True(t, p.Attention(), "the operator's signal that retirement is broken is not the absence of retirements")
	assert.Equal(t, 84, p.Exit())
	assert.Empty(t, p.Retired)
}

// THE RE-EXPORT statestore.Export names as `cycle`'s own, and the
// maintenance stage Q09 rules as Cycle.Run's last line.
func TestCycleReexportsTheNoteAndPaysItsHousekeepingBill(t *testing.T) {
	ctx := t.Context()
	repo, st := fixture(t)
	l := ledger.Open(repo)
	res := mintedChange(t, repo, st, l)
	tip := res.Ref.Tip()

	// A process that died between the Amend and its export leaves the
	// record written and no note on the commit.
	require.NoError(t, l.Remove(ctx, tip))
	_, err := l.Read(ctx, tip)
	require.Error(t, err)

	c := Cycle{Repo: repo, State: st, Ledger: l, Me: me(), Now: now, Progress: &sink{},
		Grants: Grants{Invoker: record.Human}}
	p, err := c.Run(ctx, CycleRequest{Forge: publish.ForgeRefresh})
	require.NoError(t, err)

	rec, err := l.Read(ctx, tip)
	require.NoError(t, err, "the pass re-exports what a dead process left unexported")
	assert.Equal(t, tip, rec.Sha)
	assert.True(t, p.Maintained, "the robot pays its own housekeeping bill at a moment nobody is waiting")
	assert.NoError(t, p.MaintainErr)
}

func TestDryRunPerformsNoMaintenance(t *testing.T) {
	ctx := t.Context()
	repo, st := fixture(t)
	l := ledger.Open(repo)
	mintedChange(t, repo, st, l)

	c := Cycle{Repo: repo, State: st, Ledger: l, Me: me(), Now: now, Progress: &sink{},
		Grants: Grants{Invoker: record.Human}}
	p, err := c.Run(ctx, CycleRequest{Forge: publish.ForgeRefresh, DryRun: true})
	require.NoError(t, err)
	assert.False(t, p.Maintained, "a repack is work, and a dry run performs none")
	assert.NoError(t, p.MaintainErr, "rule 7: not run is not failed")
}
