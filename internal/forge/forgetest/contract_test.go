package forgetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
)

// Each write of the stand-in leaves the state GitHub would, which the
// fault sweep compares: a pull request opened as a draft or not, edited,
// marked ready and back to draft, its reviewers asked again of it, a
// review posted, and a run's failed jobs run again, each counted once
// (docs/designs/fault-sweep.md, #129).
func TestEachWriteLeavesGitHubsState(t *testing.T) {
	t.Parallel()
	g := New("", "")
	ctx := t.Context()
	input := forge.PullRequestInput{Repository: UpstreamRepository, HeadRepository: ForkRepository, HeadBranch: "jq-1.8.1", BaseBranch: "master",
		Desired: forge.PullRequestContent{Title: "jq: update to 1.8.1", Body: "one"}, Draft: true}
	opened, err := g.Create(ctx, input)
	require.NoError(t, err)
	ref := opened.PullRequest.Ref
	require.Len(t, g.PRs, 1)
	require.True(t, g.Draft(ref.Number), "opened as a draft")
	status, err := g.Inspect(ctx, ref)
	require.NoError(t, err)
	require.True(t, status.Draft)
	_, err = g.Create(ctx, input)
	require.ErrorIs(t, err, forge.ErrRejected, "one pull request from a head to a base")
	require.Len(t, g.Created, 1)

	input.ExistingPR, input.Desired.Body = &ref, "two"
	_, err = g.Update(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "two", g.PRs[ref.Number].Body)
	require.Len(t, g.Updated, 1)

	_, err = g.MarkReady(ctx, ref)
	require.NoError(t, err)
	status, err = g.Inspect(ctx, ref)
	require.NoError(t, err)
	require.False(t, status.Draft, "marked ready, where it stayed a draft")
	_, err = g.MarkDraft(ctx, ref)
	require.NoError(t, err)
	require.True(t, g.Draft(ref.Number))
	require.Equal(t, []int{ref.Number}, g.Readied)
	require.Equal(t, []int{ref.Number}, g.Drafted)

	require.NoError(t, g.RequestReviewers(ctx, ref, []string{"ryandesign"}))
	require.Equal(t, map[int][]string{ref.Number: {"ryandesign"}}, g.RerequestedOn, "asked again of this pull request, where which one was dropped")

	_, err = g.PostReview(ctx, forge.ReviewInput{Ref: ref, Body: "fine"})
	require.NoError(t, err)
	require.Len(t, g.Reviews, 1)
	require.NoError(t, g.RerunFailed(ctx, UpstreamRepository, 7))
	require.Equal(t, []int64{7}, g.Rerun)

	// A reply lost after the pull request was opened leaves it opened.
	lost := New("", "")
	lost.CreateFails, lost.Lost = true, true
	_, err = lost.Create(ctx, input)
	require.Error(t, err)
	require.Len(t, lost.PRs, 1)
}

// The stand-in answers what #191's publication cases ask of it (#129's fix
// list, 8): a refusal apart from a failure, leaving nothing done; an
// outcome for each reviewer, a request naming one GitHub won't ask refused
// whole; an account other than ada's; and each opening's author and time.
func TestTheStandInAnswersWhatPublicationAsks(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	input := forge.PullRequestInput{Repository: UpstreamRepository, HeadRepository: ForkRepository, HeadBranch: "jq-1.8.1", BaseBranch: "master",
		Desired: forge.PullRequestContent{Title: "jq: update to 1.8.1"}}

	refusing := New("", "")
	refusing.Refuse = map[string]string{"Create": "Validation Failed"}
	_, err := refusing.Create(ctx, input)
	require.ErrorIs(t, err, forge.ErrRejected, "a refusal")
	require.Empty(t, refusing.PRs, "with nothing done")
	failing := New("", "")
	failing.CreateFails = true
	_, err = failing.Create(ctx, input)
	require.Error(t, err)
	require.NotErrorIs(t, err, forge.ErrRejected, "apart from a failure")

	at := time.Date(2026, 11, 12, 9, 30, 0, 0, time.UTC)
	g := New("", "")
	g.Person, g.Clock = "grace", func() time.Time { return at }
	login, err := g.Login(ctx)
	require.NoError(t, err)
	require.Equal(t, "grace", login.Account)
	user, err := g.AuthenticatedUser(ctx)
	require.NoError(t, err)
	require.Equal(t, "grace", user)
	opened, err := g.Create(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "grace", opened.PullRequest.Author)
	require.Equal(t, at, opened.PullRequest.CreatedAt, "the day GitHub opened it, which serve's count reads")

	ref := opened.PullRequest.Ref
	g.Unrequestable = map[string]string{"stranger": "not a collaborator"}
	err = g.RequestReviewers(ctx, ref, []string{"ryandesign", "stranger"})
	require.ErrorIs(t, err, forge.ErrRejected)
	require.ErrorContains(t, err, "stranger")
	require.Empty(t, g.Rerequested, "refused whole")
	require.NoError(t, g.RequestReviewers(ctx, ref, []string{"ryandesign"}))
	require.Equal(t, []string{"ryandesign"}, g.Rerequested)
	for _, write := range []string{"Update", "MarkReady", "MarkDraft", "PostReview", "RequestReviewers", "RerunFailed"} {
		g.Refuse = map[string]string{write: "no"}
		var err error
		switch write {
		case "Update":
			input.ExistingPR = &ref
			_, err = g.Update(ctx, input)
		case "MarkReady":
			_, err = g.MarkReady(ctx, ref)
		case "MarkDraft":
			_, err = g.MarkDraft(ctx, ref)
		case "PostReview":
			_, err = g.PostReview(ctx, forge.ReviewInput{Ref: ref, Body: "fine"})
		case "RequestReviewers":
			err = g.RequestReviewers(ctx, ref, []string{"ryandesign"})
		case "RerunFailed":
			err = g.RerunFailed(ctx, UpstreamRepository, 7)
		}
		require.ErrorIs(t, err, forge.ErrRejected, write)
	}
	require.Empty(t, g.Updated)
	require.Empty(t, g.Readied)
	require.Empty(t, g.Drafted)
	require.Empty(t, g.Reviews)
	require.Equal(t, []string{"ryandesign"}, g.Rerequested)
	require.Empty(t, g.Rerun, "each refused write left nothing done")
}
