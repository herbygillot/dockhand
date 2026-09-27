package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// v2's recovery promises for publishing, as v3 tests (roadmap item 5): a
// submit interrupted anywhere, or racing another, ends with one pull
// request, recorded, and nothing pushed over anyone.

func recordedPullRequest(t *testing.T, e *Engine, branch model.Branch) *model.PullRequest {
	t.Helper()
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	return current.PullRequest
}

// A reply lost after GitHub opened the pull request is read back at once:
// the pull request is found by its head, recorded, and not written again.
func TestALostReplyToOpeningAPullRequestIsReadBack(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	fake.createFails, fake.lost = true, true
	submitted, err := e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, 34901, submitted.PullRequest.Ref.Number)
	require.Equal(t, 34901, recordedPullRequest(t, e, branch).Number)

	again, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), again)
	require.NoError(t, err)
	require.Len(t, fake.created, 1, "opened once")
	require.Empty(t, fake.updated, "and not written again")
}

// A request that never reached GitHub leaves the commit pushed and no pull
// request; the next submit opens one, without pushing again.
func TestARequestThatNeverReachedGitHubIsMadeAgain(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	fake.createFails = true
	_, err = e.ApplySubmit(t.Context(), plan)
	require.ErrorContains(t, err, "but the pull request was not written")
	require.ErrorContains(t, err, "dockhand submit again finishes it")
	require.Empty(t, fake.prs)
	require.Nil(t, recordedPullRequest(t, e, branch))

	again, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Nil(t, again.Existing)
	submitted, err := e.ApplySubmit(t.Context(), again)
	require.NoError(t, err)
	require.False(t, submitted.Pushed, "the fork has the commit already")
	require.True(t, submitted.Created)
	require.Len(t, fake.prs, 1)
	require.Equal(t, submitted.PullRequest.Ref.Number, recordedPullRequest(t, e, branch).Number)
}

// refusingStore records nothing, as a database that fails at the last
// step would.
type refusingStore struct{ store.Store }

func (refusingStore) Update(context.Context, model.RepositoryID, func(store.Tx) error) error {
	return store.ErrUnavailable
}

// A pull request opened but not recorded, as when the record fails or the
// process ends, is found by the next submit, recorded, and left as it is.
func TestAPullRequestOpenedButNotRecordedIsFoundByTheNextSubmit(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	kept := e.Store
	e.Store = refusingStore{kept}
	_, err = e.ApplySubmit(t.Context(), plan)
	require.ErrorContains(t, err, "#34901 is open with ")
	require.ErrorContains(t, err, "but recording it failed")
	e.Store = kept
	require.Nil(t, recordedPullRequest(t, e, branch))

	again, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.NotNil(t, again.Existing, "found by its head")
	require.True(t, again.BodyKept, "what dockhand wrote isn't recorded, so its description is left as it is")
	_, err = e.ApplySubmit(t.Context(), again)
	require.NoError(t, err)
	require.Equal(t, 34901, recordedPullRequest(t, e, branch).Number)
	require.Len(t, fake.created, 1)
	require.Empty(t, fake.updated)
}

// Two submits of one branch racing, each planned before the other pushed,
// end with one pull request: the second's push is a no-op, since the fork
// holds its commit already, and GitHub's refusal of a second pull request
// is read back as the first one's.
func TestTwoSubmitsRacingOpenOnePullRequest(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	first, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	second, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)

	opened, err := e.ApplySubmit(t.Context(), first)
	require.NoError(t, err)
	joined, err := e.ApplySubmit(t.Context(), second)
	require.NoError(t, err)
	require.Equal(t, opened.PullRequest.Ref.Number, joined.PullRequest.Ref.Number)
	require.Equal(t, first.Commit, string(fake.head("dockhand/jq-update")))
	require.Len(t, fake.prs, 1)
	require.Len(t, fake.created, 1)
	require.Equal(t, opened.PullRequest.Ref.Number, recordedPullRequest(t, e, branch).Number)
}

// A branch that moved after its submit was planned is refused before
// anything is pushed; the plan is for the commit it saw.
func TestASubmitRefusesABranchThatMovedAfterItsPlan(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# more\n"})
	run(t, branch.Worktree, "commit", "-q", "-am", "jq: more")
	_, err = e.ApplySubmit(t.Context(), plan)
	require.ErrorIs(t, err, ErrStaleSubmit)
	require.Empty(t, string(fake.head("dockhand/jq-update")), "nothing was pushed")
	require.Empty(t, fake.prs)
}
