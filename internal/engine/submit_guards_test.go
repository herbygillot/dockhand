package engine

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The guards that keep submit from pushing where it shouldn't, or opening
// what MacPorts would refuse (the test plan's step 2, items 7 to 11).

// submit pushes only to your own fork of MacPorts' repository: one that
// isn't a fork of it, a remote named that isn't one, and two that could
// be are each refused, before anything is pushed.
func TestSubmitPushesOnlyToYourFork(t *testing.T) {
	t.Parallel()
	plan := func(t *testing.T, remote string, arrange func(f fixture, fake *forgetest.GitHub)) error {
		f := setup(t)
		e, _ := f.withPreparer(t)
		fake := f.withFork(t, e)
		arrange(f, fake)
		branch := committedUpdate(t, e)
		_, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Remote: remote})
		require.Empty(t, testsupport.Git(t, fake.Fork, "branch", "--list", "dockhand/*"), "nothing was pushed")
		return err
	}
	err := plan(t, "", func(_ fixture, fake *forgetest.GitHub) { fake.ForkParent = "someone/other-ports" })
	require.ErrorContains(t, err, "ada/macports-ports is not a fork of macports/macports-ports; dockhand pushes only to your fork")

	err = plan(t, "elsewhere", func(fixture, *forgetest.GitHub) {})
	require.ErrorContains(t, err, "there is no remote elsewhere that pushes to a GitHub repository other than macports/macports-ports")

	err = plan(t, "", func(f fixture, fake *forgetest.GitHub) {
		second := filepath.Join(filepath.Dir(f.upstream), "second.git")
		testsupport.Git(t, filepath.Dir(f.upstream), "clone", "-q", "--bare", f.upstream, second)
		testsupport.Git(t, f.clone, "remote", "add", "second", second)
		fake.Repos = map[string]string{"ada/ports-mirror": second}
	})
	require.ErrorContains(t, err, "several remotes push to your forks: ")
	require.ErrorContains(t, err, "choose one with --remote")
}

// With a sandbox, the acceptance test's, pull requests go to it and are
// made within it: the fork is the sandbox, which must be a fork of
// MacPorts' repository, and no other remote stands in for it.
func TestASandboxTakesThePullRequestsWithinItself(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	e.options.PullRequests = "ada/macports-ports"
	require.True(t, e.Sandboxed())
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Equal(t, "ada/macports-ports", plan.Repository)
	require.Equal(t, "ada/macports-ports", plan.HeadRepository)

	e.options.PullRequests = "tester/macports-ports"
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.ErrorContains(t, err, "no Git remote pushes to the sandbox tester/macports-ports that pull requests go to")

	e.options.PullRequests = "ada/macports-ports"
	fake.ForkParent = "someone/other-ports"
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.ErrorContains(t, err, "ada/macports-ports is not a fork of macports/macports-ports")
	require.Empty(t, testsupport.Git(t, fake.Fork, "branch", "--list", "dockhand/*"), "nothing was pushed")
}

// A branch holding a merge commit is held: MacPorts asks for a rebase.
func TestSubmitHoldsAMergeCommit(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)
	// A side commit merged in, as a pull of master would leave.
	side := testsupport.Git(t, branch.Worktree, "commit-tree", "HEAD~1^{tree}", "-p", "HEAD~1", "-m", "side")
	testsupport.Git(t, branch.Worktree, "merge", "-q", "--no-ff", "--no-edit", side)
	merge := short(model.ObjectID(testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD")))
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Contains(t, plan.Blocking, "commit "+merge+" is a merge; MacPorts asks for a rebase instead")
}

// A branch of several ports whose commits give no one title is held for
// one, and a title given settles it.
func TestSubmitHoldsSeveralPortsForATitle(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := twoPortBranch(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Contains(t, plan.Blocking, "the pull request needs a title, since the branch changes several ports: give one with --title")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Title: "jq, libharbor: update"})
	require.NoError(t, err)
	require.NotContains(t, plan.Blocking, "the pull request needs a title, since the branch changes several ports: give one with --title")
	require.Equal(t, "jq, libharbor: update", plan.Title)
}

// A rate limit is said as one to wait out, never as a login wanting
// (field testing, batch 11).
func TestARateLimitIsNotALoginWanting(t *testing.T) {
	t.Parallel()
	limited := &forge.RateLimitError{RetryAt: time.Now().Add(time.Hour), Err: errors.New("GitHub's rate limit resets at 09:46, in 60 minutes")}
	require.EqualError(t, loginError("submit", limited), "submit waits on GitHub: GitHub's rate limit resets at 09:46, in 60 minutes")
	require.EqualError(t, loginError("submit", fmt.Errorf("%w: no credentials", forge.ErrAuthentication)), "submit needs your GitHub login: forge: authentication is required: no credentials")
	require.EqualError(t, loginError("submit", errors.New("http2: timeout awaiting response headers")), "submit couldn't ask GitHub who you are: http2: timeout awaiting response headers",
		"an outage isn't a login wanting (field testing, batch 12)")
}

// A branch of an update and the rebuilds for it, as update
// --revbump-dependents makes, is titled by its update; any other branch of
// several ports still asks for one (field testing, batch 12).
func TestAnUpdateWithItsRebuildsIsTitledByTheUpdate(t *testing.T) {
	t.Parallel()
	commits := func(subjects ...string) []git.HistoryCommit {
		var all []git.HistoryCommit
		for _, subject := range subjects {
			all = append(all, git.HistoryCommit{Message: subject + "\n"})
		}
		return all
	}
	require.Equal(t, "libunibreak: update to 8.0", updateWithRebuilds(commits("libunibreak: update to 8.0", "taisei: rebuild for libunibreak 8.0", "foot: rebuild for libunibreak 8.0")))
	require.Empty(t, updateWithRebuilds(commits("jq: update to 1.8.1", "libharbor: update to 2.0")))
	require.Empty(t, updateWithRebuilds(commits("libunibreak: update to 8.0", "taisei: rebuild for libfoo 2")))
	require.Empty(t, updateWithRebuilds(commits("taisei: rebuild for libunibreak 8.0")))
}
