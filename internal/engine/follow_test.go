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
	"github.com/herbygillot/dockhand/internal/store"
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
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
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
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	e.Forge = endedLogin{fake}
	var said, notified []string
	s := newServer(e, ServeOptions{Say: func(line string) { said = append(said, line) }, Notify: func(title, text string) { notified = append(notified, title+": "+text) }, Refresh: time.Nanosecond})
	follow := &follower{s: s}
	for range 3 {
		follow.last = time.Time{}
		follow.maybe(t.Context())
	}
	require.Equal(t, []string{"serve: GitHub credentials could not be renewed, so pull request refresh is paused. Run dockhand setup github to sign in again; serve will use the new credentials automatically"}, said)
	require.Equal(t, []string{"GitHub login: GitHub credentials expired or could not be renewed. Run dockhand setup github to sign in again."}, notified)
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
		_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
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
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
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
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
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

// anonymousForge is GitHub without a login: no read over many pull
// requests, and each one read a request of an hourly allowance, which a
// rate limit ends.
type anonymousForge struct {
	*forgetest.GitHub
	allowance int
	read      []int
	inspected int
	state     map[int]forge.PullRequestState
}

func (a *anonymousForge) ReadPullRequests(context.Context, []forge.PullRequestRef) ([]forge.PullRequestReading, error) {
	return nil, github.ErrNoCredentials
}

func (a *anonymousForge) RequestsLeft(context.Context) (int, time.Time, error) {
	return a.allowance, time.Date(2026, 10, 23, 15, 4, 0, 0, time.UTC), nil
}

// Inspect reads reviews and checks, four requests more, which a refresh
// without a login never spends.
func (a *anonymousForge) Inspect(context.Context, forge.PullRequestRef) (forge.PullRequestStatus, error) {
	a.inspected++
	return forge.PullRequestStatus{}, nil
}

func (a *anonymousForge) Observe(_ context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error) {
	if a.allowance == 0 {
		return forge.PullRequestObservation{}, &forge.RateLimitError{RetryAt: time.Date(2026, 10, 23, 15, 4, 0, 0, time.UTC), Err: fmt.Errorf("rate limited")}
	}
	a.allowance--
	a.read = append(a.read, ref.Number)
	state, ok := a.state[ref.Number]
	if !ok {
		state = forge.PullRequestOpen
	}
	return forge.PullRequestObservation{Found: true, PullRequest: forge.PullRequest{Ref: ref, State: state}}, nil
}

// Signed out, a refresh reads the open branches' pull requests, the
// newest first, and none of the closed ones, which a signed-in refresh
// reads: logged out, serve read 15 closed test pull requests, spent
// GitHub's anonymous 60 an hour, and never saw #88 close (rc5's D-C2).
// Where the limit stops it, it says how many it left and when.
func TestASignedOutRefreshReadsOpenPullRequestsNewestFirst(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	base := committedUpdate(t, e)
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		for i := range 16 {
			branch := base
			branch.ID, branch.Name = model.BranchID(store.NewID("br")), fmt.Sprintf("%sjq-%d", model.BranchPrefix, i)
			branch.Worktree = filepath.Join(t.TempDir(), "jq")
			branch.State, branch.EndedAt = model.BranchClosed, time.Now()
			if i == 15 {
				branch.State, branch.EndedAt = model.BranchOpen, time.Time{}
			}
			branch.PullRequest = &model.PullRequest{Repository: "macports/macports-ports", Number: 73 + i, Head: "ada/macports-ports:" + branch.Name}
			if err := tx.AddBranch(branch); err != nil {
				return err
			}
		}
		return nil
	}))
	anonymous := &anonymousForge{GitHub: forgetest.New(f.upstream, ""), allowance: 60, state: map[int]forge.PullRequestState{88: forge.PullRequestClosed}}
	e.Forge = anonymous
	refreshed, err := e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, []int{88}, anonymous.read, "the open one, and no closed one")
	require.Len(t, refreshed.Branches, 1)
	require.Equal(t, []string{"#88 is closed"}, refreshed.Branches[0].Changes, "and it sees #88 close")
	require.Equal(t, model.BranchClosed, refreshed.Branches[0].Branch.State)
	require.Empty(t, refreshed.UnreadWords())

	// With the allowance spent, the refresh stops, and says what's left.
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		branch := base
		branch.ID, branch.Name, branch.Worktree = model.BranchID(store.NewID("br")), model.BranchPrefix+"jq-new", filepath.Join(t.TempDir(), "new")
		branch.PullRequest = &model.PullRequest{Repository: "macports/macports-ports", Number: 99, Head: "ada/macports-ports:dockhand/jq-new"}
		return tx.AddBranch(branch)
	}))
	anonymous.allowance = 0
	refreshed, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, refreshed.Unread)
	require.Equal(t, "GitHub's rate limit prevented refresh of 1 pull request. Dockhand will retry after "+time.Date(2026, 10, 23, 15, 4, 0, 0, time.UTC).Local().Format("15:04 MST"), refreshed.UnreadWords())

	// Each read is one request, the state and head alone, the review and
	// checks recorded kept; and it stops 10 short of the hour's allowance
	// (rc5's D-C2 again: five requests a pull request a refresh spent the
	// hour on one).
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
		for i := range 4 {
			branch := base
			branch.ID, branch.Name, branch.Worktree = model.BranchID(store.NewID("br")), fmt.Sprintf("%sjq-open-%d", model.BranchPrefix, i), filepath.Join(t.TempDir(), "open")
			branch.PullRequest = &model.PullRequest{Repository: "macports/macports-ports", Number: 100 + i, Head: "ada/macports-ports:" + branch.Name,
				Observed: &model.PullRequestObservation{State: model.PullRequestOpen, Review: model.ReviewChangesRequested, ChangesRequestedBy: []string{"ryan"}, Checks: model.ChecksFailing, Failing: []string{"macOS 15"}}}
			if err := tx.AddBranch(branch); err != nil {
				return err
			}
		}
		return nil
	}))
	anonymous.allowance, anonymous.read = 12, nil
	refreshed, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, []int{103, 102}, anonymous.read, "12 left, 10 kept: two reads, the newest first")
	require.Zero(t, anonymous.inspected, "no review or checks read")
	require.Equal(t, 3, refreshed.Unread, "#101, #100, and #99 wait")
	for _, r := range refreshed.Branches {
		require.Empty(t, r.Changes, "nothing new said of a review or checks not read")
		require.Equal(t, model.ReviewChangesRequested, r.Branch.PullRequest.Observed.Review)
		require.Equal(t, []string{"macOS 15"}, r.Branch.PullRequest.Observed.Failing)
	}
}
