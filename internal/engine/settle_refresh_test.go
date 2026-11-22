package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Where status reads a pull request anyway, that reading settles what it
// can, under the head's lock, with no read of its own: a ready GitHub
// shows done is folded and the step leaves the publication, a step still
// owed keeps it, a head another process holds leaves it as it is, and the
// pull request merged ends it (#191, group A; docs/designs/
// publication-record.md, "When settling runs").
func TestARefreshSettlesWhatItRead(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number
	require.False(t, fake.Draft(number))

	recorded := plan.publication("pub_refresh", "", false)
	recorded.Number = number
	recorded.Review = &model.ReviewAsk{Answer: model.ReviewYes, Reviewers: []model.Reviewer{{Login: "reviewer"}}}
	recorded.Owes = []model.PublicationStep{model.StepReady, model.StepRerequest}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.SetPublication(branch.ID, "", &recorded) }))
	publication := func() *model.Publication {
		current, err := e.Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		return current.Publication
	}

	// Another process holds the head: the refresh reads and writes the
	// pull request, and leaves the publication to that process.
	require.NoError(t, e.Repo.WithHeadLock(t.Context(), recorded.Head, func(context.Context) error {
		_, err := e.RefreshPullRequests(t.Context())
		return err
	}))
	require.Equal(t, recorded.Owes, publication().Owes, "a held head is left as it is")

	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	kept := publication()
	require.NotNil(t, kept, "the reviewers are still owed")
	require.Equal(t, []model.PublicationStep{model.StepRerequest}, kept.Owes, "GitHub shows it ready")

	fake.PRs[number].State = forge.PullRequestMerged
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Nil(t, publication(), "a merged pull request ends it")
}
