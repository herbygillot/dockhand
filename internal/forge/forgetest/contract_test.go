package forgetest

import (
	"testing"

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
