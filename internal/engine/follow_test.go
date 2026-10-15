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
	t.Parallel()
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
	require.NoError(t, refreshed.Refused)
	require.Len(t, refreshed.Branches, 1)
	require.NoError(t, refreshed.Branches[0].Err)
	require.Equal(t, []string{"#34901 is open", "#34901: changes requested", "#34901: CI failing"}, refreshed.Branches[0].Changes)
	statuses, err := e.Status(t.Context())
	require.NoError(t, err)
	observed := statuses[0].Branch.PullRequest.Observed
	require.Equal(t, model.ReviewChangesRequested, observed.Review)
	require.Equal(t, []string{"macOS 15"}, statuses[0].Failing())
	require.False(t, statuses[0].SomeoneElsePushed())

	again, err := e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Empty(t, again.Branches[0].Changes, "nothing new, nothing said")

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
	require.EqualError(t, refreshed.Branches[0].Err, "#34901 was not found")
	fake.PRs[number] = pr

	// And it is merged.
	fake.PRs[number].State = forge.PullRequestMerged
	refreshed, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"#34901 is merged"}, refreshed.Branches[0].Changes)
	require.Equal(t, model.BranchMerged, refreshed.Branches[0].Branch.State)
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
	t.Parallel()
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
	require.Equal(t, []string{"serve: dockhand's GitHub login can't renew itself, so pull requests can't be read; run dockhand setup github, which serve uses without a restart"}, said)
	require.Equal(t, []string{"GitHub login: dockhand's GitHub login can't renew itself; run dockhand setup github"}, notified)
}

// A credential GitHub rejects fails the read once, as its error, not each
// pull request's: a revoked login was said five times, and status went on
// to exit 0 (the rc6 full stage, D-C3).
func TestARejectedLoginFailsTheReadOnce(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	for _, name := range []string{"jq-one", "jq-two"} {
		branch, err := e.Start(t.Context(), StartRequest{Name: name})
		require.NoError(t, err)
		_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
		require.NoError(t, err)
		tidy, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
		require.NoError(t, err)
		_, err = e.ApplyTidy(t.Context(), tidy)
		require.NoError(t, err)
		plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
		require.NoError(t, err)
		_, err = e.ApplySubmit(t.Context(), plan)
		require.NoError(t, err)
	}
	fake.ObserveErr = fmt.Errorf("%w: GitHub rejected the credential from Dockhand macOS Keychain", github.ErrAuthentication)
	fake.Observed = 0
	refreshed, err := e.RefreshPullRequests(t.Context())
	require.NoError(t, err, "the store's error alone")
	require.ErrorIs(t, refreshed.Refused, github.ErrAuthentication)
	require.Empty(t, refreshed.Branches)
	require.Equal(t, 1, fake.Observed, "the read stops at the first rejection")
}

// The pull requests are stale for status where what's recorded of an
// open branch's is older than serve reads them, or was never read; a drain
// names the open ones it leaves unwatched (SQL-O8).
func TestStalePullRequestsAreReadAndADrainNamesTheUnwatched(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	stale, err := e.PullRequestsStale(t.Context())
	require.NoError(t, err)
	require.False(t, stale, "no pull request, nothing stale")
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	stale, err = e.PullRequestsStale(t.Context())
	require.NoError(t, err)
	require.True(t, stale, "never read")
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	stale, err = e.PullRequestsStale(t.Context())
	require.NoError(t, err)
	require.False(t, stale, "just read")
	later := time.Now().Add(PullRequestRefresh + time.Minute)
	e.options.Now = func() time.Time { return later }
	stale, err = e.PullRequestsStale(t.Context())
	require.NoError(t, err)
	require.True(t, stale, "older than serve reads them")
	require.Equal(t, []string{"#34901"}, e.followedPullRequests(t.Context()))
}

// batchReader is GitHub reading every pull request at once, as GraphQL
// does, from the fake's pull requests; partial names those whose reading
// couldn't say everything, which are read one at a time.
type batchReader struct {
	*forgetest.GitHub
	partial map[int]bool
	read    int
}

func (b *batchReader) ReadPullRequests(ctx context.Context, refs []forge.PullRequestRef) ([]forge.PullRequestReading, error) {
	b.read++
	var readings []forge.PullRequestReading
	for _, ref := range refs {
		pr, ok := b.PRs[ref.Number]
		if !ok {
			readings = append(readings, forge.PullRequestReading{Ref: ref})
			continue
		}
		status, _ := b.Inspect(ctx, ref)
		readings = append(readings, forge.PullRequestReading{Ref: ref, Found: true, Partial: b.partial[ref.Number], PullRequest: *pr, Status: status})
	}
	return readings, nil
}

// Where the forge reads every pull request at once, the refresh reads
// them so, and reads one at a time only one whose reading couldn't say
// everything (SQL-O8's GraphQL read).
func TestPullRequestsAreReadAtOnceWhereTheForgeCan(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number
	fake.Statuses = map[int]forge.PullRequestStatus{number: {Review: "approved", Approvals: 1, Checks: forge.CheckSummary{Total: 2, Passed: 2}}}
	reader := &batchReader{GitHub: fake, partial: map[int]bool{}}
	e.Forge = reader
	fake.Observed = 0
	refreshed, err := e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, reader.read)
	require.Zero(t, fake.Observed, "not read one at a time")
	require.Equal(t, []string{"#34901 is open", "#34901: approved", "#34901: CI passing"}, refreshed.Branches[0].Changes)

	reader.partial[number] = true
	fake.Statuses[number] = forge.PullRequestStatus{Review: "approved", Approvals: 1, Checks: forge.CheckSummary{Total: 2, Passed: 1, Failed: 1, Failing: []string{"macOS 15"}}}
	refreshed, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, fake.Observed, "a partial reading is read again by itself")
	require.Equal(t, []string{"#34901: CI failing"}, refreshed.Branches[0].Changes)

	delete(fake.PRs, number)
	refreshed, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.EqualError(t, refreshed.Branches[0].Err, "#34901 was not found")
}
