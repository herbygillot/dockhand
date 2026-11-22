package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A follow-up reads the pull request's head itself, just before its call:
// where someone moved the fork's branch after the push, marking ready was
// for the commit submitted, so it isn't made, the publication ends, and
// what it owed comes back as dropped (#191, group B; docs/designs/
// publication-record.md, "Follow-ups are for the commit").
func TestAFollowUpIsForTheCommitSubmitted(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Draft: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number
	require.True(t, fake.Draft(number))

	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Ready: true})
	require.NoError(t, err)
	base := testsupport.Git(t, f.clone, "rev-parse", "origin/master")
	fake.Observing = func(ref forge.PullRequestRef) {
		if ref.Number == number {
			testsupport.Git(t, fake.Fork, "update-ref", "refs/heads/"+plan.RemoteBranch(), base)
		}
	}
	submitted, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Equal(t, model.ObjectID(base), submitted.MovedTo)
	require.Equal(t, []model.PublicationStep{model.StepReady}, submitted.Dropped)
	require.False(t, submitted.Ready)
	require.Empty(t, fake.Readied, "ready was for the commit submitted")
	require.True(t, fake.Draft(number))
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Nil(t, branch.Publication, "the head at another commit ends the publication")
}
