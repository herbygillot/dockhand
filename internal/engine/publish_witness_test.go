package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// The witness of a publication write compares the whole record, the
// reviewers' marks among it: it compared the ID and the steps owed, so a
// reviewer mark that didn't commit passed, and the ask went out with no
// mark (#191, group A).
func TestAPublicationWriteIsWitnessedWhole(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "notes"})
	require.NoError(t, err)
	at := time.Date(2026, 11, 22, 8, 0, 0, 0, time.FixedZone("EST", -5*3600))
	recorded := &model.Publication{ID: "pub_1", Commit: "abc", At: at, Head: "ada/macports-ports:dockhand/notes", Base: "macports/macports-ports:master",
		Review: &model.ReviewAsk{Answer: model.ReviewYes, Reviewers: []model.Reviewer{{Login: "reviewer", Reviewed: at, Sent: model.NotSent}}},
		Owes:   []model.PublicationStep{model.StepRerequest}}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.SetPublication(branch.ID, "", recorded) }))

	read := func(want *model.Publication) bool {
		var same bool
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			same = publicationIs(branch.ID, want)(r)
			return nil
		}))
		return same
	}
	require.True(t, read(recorded), "the record as written, read back across the store")

	marked := *recorded
	review := *recorded.Review
	review.Reviewers = []model.Reviewer{{Login: "reviewer", Reviewed: at, Sent: model.MaySent}}
	marked.Review = &review
	require.False(t, read(&marked), "a mark the store doesn't hold isn't witnessed")

	retitled := *recorded
	retitled.Title = "jq: update to 1.8.1"
	require.False(t, read(&retitled), "any other field the store doesn't hold isn't either")
}
