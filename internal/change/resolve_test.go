package change

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/statestore"
)

func TestResolveAnswersToAnIdABranchAPortAndAPin(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")

	for _, target := range []string{"chg-01", "dockhand/jq-1.8", "jq"} {
		t.Run(target, func(t *testing.T) {
			ref, err := Resolve(ctx, repo, st, target)
			require.NoError(t, err)
			assert.True(t, ref.Valid())
			assert.Equal(t, record.ChangeID("chg-01"), ref.ID())
			assert.Equal(t, "dockhand/jq-1.8", ref.Branch())
			assert.Equal(t, c.Tip, ref.Tip())
			assert.Equal(t, c.Content, ref.Content())
			assert.Same(t, repo, ref.Repo())
			require.Len(t, ref.Subjects(), 1)
			assert.Equal(t, "jq", ref.Subjects()[0].Port)
		})
	}

	// The pin names the branchless record by its id, which is the one
	// target a port cannot be a name for: a port may carry a snapshot and
	// a branch change at once.
	sha, content, err := Snapshot(ctx, repo, "chg-02", repo.Root+"/sysutils/jq")
	require.NoError(t, err)
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		_, err := AdoptIn(tx, Adoption{ID: "chg-02", Tip: sha, Content: content,
			Subjects: []record.Subject{{Port: "jq", Portdir: "sysutils/jq"}}}, now)
		return err
	}))
	ref, err := Resolve(ctx, repo, st, PinRef("chg-02"))
	require.NoError(t, err)
	assert.Equal(t, record.ChangeID("chg-02"), ref.ID())
	assert.Empty(t, ref.Branch())
	assert.Equal(t, sha, ref.Tip())
}

func TestResolveRefusesABranchTheStoreDoesNotKnow(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	plant(t, repo, "branch", "dockhand/hand-made")

	_, err := Resolve(ctx, repo, st, "dockhand/hand-made")
	require.ErrorIs(t, err, ErrNoRecord, "Verify's adopt stage answers this; every other road refuses it")
	_, err = Resolve(ctx, repo, st, "nothing-at-all")
	assert.ErrorIs(t, err, ErrNoRecord)
}

func TestResolveReportsADisagreementRatherThanTrustingEitherSide(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")

	moved := byHand(t, repo, "dockhand/jq-1.8", c.Tip, "version 1.8.1\n", "by hand")
	_, err := Resolve(ctx, repo, st, "dockhand/jq-1.8")
	require.ErrorIs(t, err, ErrTipDisagrees)
	var d *TipDisagreement
	require.ErrorAs(t, err, &d)
	assert.Equal(t, record.ChangeID("chg-01"), d.ID)
	assert.Equal(t, BranchRef("dockhand/jq-1.8"), d.Ref)
	assert.Equal(t, c.Tip, d.Recorded)
	assert.Equal(t, moved, d.Found)
	assert.False(t, d.Absent, "a moved ref and a deleted one are two answers, not one empty string")

	plant(t, repo, "update-ref", "-d", "refs/heads/dockhand/jq-1.8")
	_, err = Resolve(ctx, repo, st, "chg-01")
	require.ErrorAs(t, err, &d)
	assert.True(t, d.Absent)
	assert.Empty(t, d.Found)
}

func TestResolvePrefersTheBoundRecordWhereTwoCarryOneName(t *testing.T) {
	repo, st := newRepo(t)
	ctx := context.Background()
	// The closed record keeps the branch name it had as history; the live
	// one binds it. Names are reused, ids are not.
	c := minted(t, repo, st, "chg-01", "dockhand/jq-1.8")
	require.NoError(t, st.Amend(ctx, func(tx *statestore.Txn) error {
		if err := CloseIn(tx, "chg-01", record.ChangeDiscarded, "", now); err != nil {
			return err
		}
		// The old record's branch line is DemolishIn's, in the same batch:
		// without it the create line below meets a ref that still stands,
		// which is git's own judgment and the right one.
		if err := DemolishIn(tx, "chg-01", now); err != nil {
			return err
		}
		_, err := MintIn(tx, Minting{
			ID: "chg-02", Branch: "dockhand/jq-1.8", Tip: c.Tip, Content: c.Content,
			Subjects: []record.Subject{{Port: "jq"}}, Destination: record.ToBranch,
		}, now)
		return err
	}))
	ref, err := Resolve(ctx, repo, st, "dockhand/jq-1.8")
	require.NoError(t, err)
	assert.Equal(t, record.ChangeID("chg-02"), ref.ID())
}

func TestStandingAnswersFromTheStoreAndPicksTheNewest(t *testing.T) {
	s := statestore.State{Changes: map[string]record.Change{
		"chg-01": {ID: "chg-01", State: record.ChangeDiscarded, Subjects: []record.Subject{{Port: "jq"}}},
		"chg-02": {ID: "chg-02", State: record.ChangeMinted, Subjects: []record.Subject{{Port: "jq"}}},
		"chg-03": {ID: "chg-03", State: record.ChangeMinted, Subjects: []record.Subject{{Port: "mise"}}},
	}}
	got, ok := Standing(s, "jq")
	require.True(t, ok)
	assert.Equal(t, record.ChangeID("chg-02"), got.ID, "a closed record does not stand")

	s.Changes["chg-04"] = record.Change{ID: "chg-04", State: record.ChangeMinted, Subjects: []record.Subject{{Port: "jq"}}}
	got, ok = Standing(s, "jq")
	require.True(t, ok)
	assert.Equal(t, record.ChangeID("chg-04"), got.ID, "the newest wins, deterministically, because --replace reads this")

	_, ok = Standing(s, "nothing")
	assert.False(t, ok)

	// A superseded record has given its name up even while it is open.
	s.Changes["chg-04"] = record.Change{ID: "chg-04", State: record.ChangeMinted, SupersededBy: "chg-05",
		Subjects: []record.Subject{{Port: "jq"}}}
	got, ok = Standing(s, "jq")
	require.True(t, ok)
	assert.Equal(t, record.ChangeID("chg-02"), got.ID)
}

func TestStandingFindsAPortByItsSubportName(t *testing.T) {
	s := statestore.State{Changes: map[string]record.Change{
		"chg-01": {ID: "chg-01", State: record.ChangeMinted,
			Subjects: []record.Subject{{Port: "py-foo", Names: []string{"py-foo", "py312-foo"}}}},
	}}
	_, ok := Standing(s, "py312-foo")
	assert.True(t, ok, "a person naming a subport means the member that owns it")
}
