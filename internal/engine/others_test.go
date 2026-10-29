package engine

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
)

// A port's open pull requests are searched once a run, whoever asks:
// start's or an update's look, started beside its work, serves the next
// ask, until it has stood othersKept. A failed search isn't kept, and
// submit's look searches afresh, since one may have opened while the
// branch was checked (the person's "Add this to 0.7.0 as well",
// 2026-10-29).
func TestAPortsOpenPullRequestsAreSearchedOnceARun(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	e.options.Now = func() time.Time { return now }
	fake.Others = []forge.PullRequestSummary{{Number: 34620, Title: "jq: update to 1.8.0"}, {Number: 34700, Title: "jqp: update to 2"}}

	e.LookForOthers(t.Context(), []string{"jq"})
	require.Equal(t, []forge.PullRequestSummary{{Number: 34620, Title: "jq: update to 1.8.0"}}, e.OtherPullRequests(t.Context(), []string{"jq"}, 0), "jqp isn't jq")
	others, problem := e.openPullRequests(t.Context(), []string{"jq"}, 0)
	require.Empty(t, problem)
	require.Len(t, others, 1)
	require.Empty(t, e.OtherPullRequests(t.Context(), []string{"jq"}, 34620), "the branch's own is no other")
	require.Equal(t, 1, fake.Searches, "searched once")

	_, problem = e.pullRequestsNow(t.Context(), []string{"jq"}, 0)
	require.Empty(t, problem)
	require.Equal(t, 2, fake.Searches, "submit's look searches afresh")
	now = now.Add(othersKept)
	e.OtherPullRequests(t.Context(), []string{"jq"}, 0)
	require.Equal(t, 3, fake.Searches, "an answer stands its time")

	fake.SearchErr = errors.New("rate limited")
	require.Empty(t, e.OtherPullRequests(t.Context(), []string{"libuv"}, 0), "a failed search says nothing")
	_, problem = e.openPullRequests(t.Context(), []string{"libuv"}, 0)
	require.Equal(t, "rate limited", problem, "an unattended run's holds still hear it")
	require.Equal(t, 5, fake.Searches, "a failed search isn't kept")
}
