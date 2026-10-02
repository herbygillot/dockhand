package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestPullRequestsAreFollowed(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Equal(t, []string{"jq"}, plan.Searched, "the ports other pull requests were looked for under")
	submitted, err := e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number

	fake.Statuses = map[int]forge.PullRequestStatus{number: {Review: "changes-requested", ChangesRequested: 1,
		Checks: forge.CheckSummary{Total: 3, Passed: 2, Failed: 1, Failing: []string{"macOS 15"}}}}
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
	testsupport.Git(t, t.TempDir(), "clone", "-q", "-b", "dockhand/jq-update", fake.Fork, other)
	write(t, other, map[string]string{"README": "theirs\n"})
	testsupport.Git(t, other, "add", "README")
	testsupport.Git(t, other, "commit", "-q", "-m", "their change")
	testsupport.Git(t, other, "push", "-q", "origin", "dockhand/jq-update")
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	statuses, err = e.Status(t.Context())
	require.NoError(t, err)
	require.True(t, statuses[0].SomeoneElsePushed())

	// The forge no longer has it.
	pr := fake.PRs[number]
	delete(fake.PRs, number)
	refreshed, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.EqualError(t, refreshed[0].Err, "#34901 was not found")
	fake.PRs[number] = pr

	// And it is merged.
	fake.PRs[number].State = forge.PullRequestMerged
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

// endedLogin is GitHub through a login that can't renew itself.
type endedLogin struct{ *forgetest.GitHub }

func (endedLogin) Observe(context.Context, forge.PullRequestRef) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, fmt.Errorf("Get \"https://api.github.com/repos/macports/macports-ports/pulls/34901\": %w", github.ErrLoginEnded)
}

// A login that can't renew itself is said once, as one problem rather
// than each pull request's, and notified once a process (the GitHub auth
// flow review's plan, step 4).
func TestServeSaysAnEndedLoginOnce(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	e.Forge = endedLogin{fake}
	var said, notified []string
	s := newServer(e, ServeOptions{Say: func(line string) { said = append(said, line) }, Notify: func(title, text string) { notified = append(notified, title+": "+text) }, Refresh: time.Nanosecond})
	follow := &follower{s: s}
	for range 3 {
		follow.last = time.Time{}
		follow.maybe(t.Context())
	}
	require.Equal(t, []string{"serve: dockhand's GitHub login can't renew itself, so pull requests can't be read; run dockhand auth login, which serve uses without a restart"}, said)
	require.Equal(t, []string{"GitHub login: dockhand's GitHub login can't renew itself; run dockhand auth login"}, notified)
}
