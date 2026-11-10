package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
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
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	fake.CreateFails, fake.Lost = true, true
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Equal(t, 34901, submitted.PullRequest.Ref.Number)
	require.Equal(t, 34901, recordedPullRequest(t, e, branch).Number)

	again, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), again, model.AnsweredAsked)
	require.NoError(t, err)
	require.Len(t, fake.Created, 1, "opened once")
	require.Empty(t, fake.Updated, "and not written again")
}

// A request that never reached GitHub leaves the commit pushed and no pull
// request; the next submit opens one, without pushing again.
func TestARequestThatNeverReachedGitHubIsMadeAgain(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	fake.CreateFails = true
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.ErrorContains(t, err, "but could not create or update the pull request")
	require.ErrorContains(t, err, "run dockhand submit again to finish")
	require.Empty(t, fake.PRs)
	require.Nil(t, recordedPullRequest(t, e, branch))

	again, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Nil(t, again.Existing)
	submitted, err := e.ApplySubmit(t.Context(), again, model.AnsweredAsked)
	require.NoError(t, err)
	require.False(t, submitted.Pushed, "the fork has the commit already")
	require.True(t, submitted.Created)
	require.Len(t, fake.PRs, 1)
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
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	kept := e.Store
	e.Store = refusingStore{kept}
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.ErrorContains(t, err, "#34901 is open with commit ")
	require.ErrorContains(t, err, "but dockhand could not save its record")
	e.Store = kept
	require.Nil(t, recordedPullRequest(t, e, branch))

	again, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.NotNil(t, again.Existing, "found by its head")
	require.True(t, again.BodyKept, "what dockhand wrote isn't recorded, so its description is left as it is")
	_, err = e.ApplySubmit(t.Context(), again, model.AnsweredAsked)
	require.NoError(t, err)
	require.Equal(t, 34901, recordedPullRequest(t, e, branch).Number)
	require.Len(t, fake.Created, 1)
	require.Empty(t, fake.Updated)
}

// Two submits of one branch racing, each planned before the other pushed,
// end with one pull request: the second's push is a no-op, since the fork
// holds its commit already, and GitHub's refusal of a second pull request
// is read back as the first one's.
func TestTwoSubmitsRacingOpenOnePullRequest(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	first, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	second, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)

	opened, err := e.ApplySubmit(t.Context(), first, model.AnsweredAsked)
	require.NoError(t, err)
	joined, err := e.ApplySubmit(t.Context(), second, model.AnsweredAsked)
	require.NoError(t, err)
	require.Equal(t, opened.PullRequest.Ref.Number, joined.PullRequest.Ref.Number)
	require.Equal(t, first.Commit, string(fake.ForkHead("dockhand/jq-update")))
	require.Len(t, fake.PRs, 1)
	require.Len(t, fake.Created, 1)
	require.Equal(t, opened.PullRequest.Ref.Number, recordedPullRequest(t, e, branch).Number)
}

// A branch that moved after its submit was planned is refused before
// anything is pushed; the plan is for the commit it saw.
func TestASubmitRefusesABranchThatMovedAfterItsPlan(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# more\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-am", "jq: more")
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.ErrorIs(t, err, ErrStaleSubmit)
	require.Empty(t, string(fake.ForkHead("dockhand/jq-update")), "nothing was pushed")
	require.Empty(t, fake.PRs)
}

// lostReply is a forge whose pull request update GitHub takes, while its
// reply is lost, as long as lose is set.
type lostReply struct {
	*forgetest.GitHub
	lose bool
}

func (g *lostReply) Update(ctx context.Context, in forge.PullRequestInput) (forge.PullRequestObservation, error) {
	observed, err := g.GitHub.Update(ctx, in)
	if err == nil && g.lose {
		return forge.PullRequestObservation{}, errors.New("the reply was lost")
	}
	return observed, err
}

// A submit whose record failed after GitHub took its description, or whose
// update's reply was lost, leaves the description dockhand's: the next
// submit finishes the record, and a later one still refreshes it. The
// retry read its own text as a person's edit, GitHub's differing from the
// record a submit behind, and never wrote the description again (#127).
func TestADescriptionWrittenButNotRecordedStaysDockhands(t *testing.T) {
	t.Parallel()
	for _, lost := range []bool{false, true} {
		f := setup(t)
		e, _ := f.withPreparer(t)
		fake := f.withFork(t, e)
		forgeDouble := &lostReply{GitHub: fake}
		e.Forge = forgeDouble
		branch := committedUpdate(t, e)
		plan := func() SubmitPlan {
			t.Helper()
			current, err := e.Branch(t.Context(), branch.ID)
			require.NoError(t, err)
			plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: current, NoCheck: true})
			require.NoError(t, err)
			return plan
		}
		commit := func(lines, subject string) {
			t.Helper()
			testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n" + lines})
			testsupport.Git(t, branch.Worktree, "commit", "-qam", subject)
		}
		_, err := e.ApplySubmit(t.Context(), plan(), model.AnsweredAsked)
		require.NoError(t, err)

		commit("# drop the patch\n", "jq: drop the patch")
		second := plan()
		if lost {
			forgeDouble.lose = true
			_, err = e.ApplySubmit(t.Context(), second, model.AnsweredAsked)
			require.ErrorContains(t, err, "the reply was lost")
			forgeDouble.lose = false
		} else {
			kept := e.Store
			e.Store = refusingStore{kept}
			_, err = e.ApplySubmit(t.Context(), second, model.AnsweredAsked)
			require.ErrorContains(t, err, "but dockhand could not save its record")
			e.Store = kept
		}
		require.Contains(t, fake.PRs[34901].Body, "drop the patch", "GitHub took the description")

		again := plan()
		require.Equal(t, SectionCurrent, again.Sections.Description, "lost %v: its own text, already there", lost)
		_, err = e.ApplySubmit(t.Context(), again, model.AnsweredAsked)
		require.NoError(t, err)

		commit("# drop the patch\n# and the docs\n", "jq: and the docs")
		third := plan()
		require.Equal(t, SectionRefreshed, third.Sections.Description, "lost %v", lost)
		_, err = e.ApplySubmit(t.Context(), third, model.AnsweredAsked)
		require.NoError(t, err)
		require.Contains(t, fake.PRs[34901].Body, "and the docs")
	}
}
