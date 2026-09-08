package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
)

// THE NOTE IS WRITTEN BY THE OPERATION THAT CHANGED A COMMIT-BOUND FACT,
// which is statestore.Export's own answer to "who calls it". Before this,
// the only production caller was the settle path, so a `--no-verify`
// bump left a branch whose commit carried no note at all and a reviewer
// running `git log --notes=dockhand/verify` on it saw nothing where the
// shipped tool showed the change record.
func TestAMintExportsTheNoteAReviewerReads(t *testing.T) {
	ctx := t.Context()
	repo, st := fixture(t)
	l := ledger.Open(repo)
	res := mintedChange(t, repo, st, l)

	rec, err := l.Read(ctx, res.Ref.Tip())
	require.NoError(t, err, "a minted branch carries the record a reviewer reads")
	assert.Equal(t, res.Ref.Tip(), rec.Sha)
	require.Len(t, rec.Change.Subjects, 1)
	assert.Equal(t, "jq", rec.Change.Subjects[0].Port)
	assert.Equal(t, record.ChangeMinted, rec.Change.State)
}

// A DISCARD REMOVES THE NOTE IT COULD NEVER CORRECT LATER. The change is
// closed and its record is compactable; once `cycle --compact` drops it,
// a re-export answers statestore.ErrNoChangeAt forever and the stale note
// describes a change the store no longer holds.
func TestDiscardRemovesTheNoteItCouldNotCorrectLater(t *testing.T) {
	ctx := t.Context()
	repo, st := fixture(t)
	l := ledger.Open(repo)
	res := mintedChange(t, repo, st, l)
	tip := res.Ref.Tip()

	// Written whatever the mint did, so that what this test measures is
	// the REMOVAL and not the export beside it.
	require.NoError(t, st.Export(ctx, l, tip))
	_, err := l.Read(ctx, tip)
	require.NoError(t, err)

	d := Discard{Repo: repo, State: st, Ledger: l, Me: me(), Now: now, Invoker: record.Human, Progress: &sink{}}
	out, err := d.Run(ctx, res.Ref.Branch())
	require.NoError(t, err)
	require.Equal(t, res.Ref.ID(), out.Closed)

	_, err = l.Read(ctx, tip)
	assert.Error(t, err, "the note goes with the change it describes")
}
