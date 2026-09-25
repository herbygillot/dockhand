package engine

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/record"
)

func TestPullRequestsAreFollowed(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number

	fake.statuses = map[int]record.PullRequestStatus{number: {Review: "changes-requested", ChangesRequested: 1,
		Checks: record.CheckSummary{Total: 3, Passed: 2, Failed: 1, Failing: []string{"macOS 15"}}}}
	refreshed, err := e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Len(t, refreshed, 1)
	require.NoError(t, refreshed[0].Err)
	require.Equal(t, []string{"#34901 is open", "#34901: changes requested", "#34901: CI failing"}, refreshed[0].Changes)
	statuses, err := e.Status(t.Context())
	require.NoError(t, err)
	observed := statuses[0].Branch.PullRequest.Observed
	require.Equal(t, "changes-requested", observed.Review)
	require.Equal(t, []string{"macOS 15"}, statuses[0].Failing())
	require.False(t, statuses[0].SomeoneElsePushed())

	again, err := e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Empty(t, again[0].Changes, "nothing new, nothing said")

	// Someone else pushes to the pull request's branch.
	other := filepath.Join(t.TempDir(), "other")
	run(t, t.TempDir(), "clone", "-q", "-b", "dockhand/jq-update", fake.fork, other)
	write(t, other, map[string]string{"README": "theirs\n"})
	run(t, other, "add", "README")
	run(t, other, "commit", "-q", "-m", "their change")
	run(t, other, "push", "-q", "origin", "dockhand/jq-update")
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	require.True(t, statuses[0].SomeoneElsePushed())

	// And it is merged.
	fake.prs[number].State = record.PullRequestMerged
	refreshed, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"#34901 is merged"}, refreshed[0].Changes)
	require.Equal(t, model.BranchMerged, refreshed[0].Branch.State)
	open, err := e.Status(t.Context())
	require.NoError(t, err)
	require.Empty(t, open, "a merged branch leaves the open list")
	merged, err := e.Status(t.Context(), model.BranchMerged)
	require.NoError(t, err)
	require.Len(t, merged, 1, "and stays searchable")
}
