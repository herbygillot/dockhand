package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/record"
)

// fakeGitHub stands in for GitHub: the fork is a local bare repository.
type fakeGitHub struct {
	upstream, fork string
	prs            []record.PullRequest
	drafts         []bool
	status         record.PullRequestStatus
}

func (g *fakeGitHub) AuthenticatedUser(context.Context) (string, error) { return "ada", nil }
func (g *fakeGitHub) NameFromRemote(url string) (string, error) {
	switch url {
	case g.upstream:
		return engine.UpstreamRepository, nil
	case g.fork:
		return "ada/macports-ports", nil
	}
	return "", errors.New("not GitHub")
}
func (g *fakeGitHub) RepositoryInfo(_ context.Context, name string) (forge.RepositoryInfo, error) {
	return forge.RepositoryInfo{Name: name, Parent: engine.UpstreamRepository}, nil
}
func (g *fakeGitHub) Find(context.Context, forge.PullRequestQuery) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, nil
}
func (g *fakeGitHub) Observe(_ context.Context, ref record.PullRequestRef) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{Found: true, PullRequest: g.prs[ref.Number-34901]}, nil
}
func (g *fakeGitHub) Create(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	number := 34901 + len(g.prs)
	pr := record.PullRequest{Ref: record.PullRequestRef{Repository: input.Repository, Number: number, URL: fmt.Sprintf("https://github.com/%s/pull/%d", input.Repository, number)},
		State: record.PullRequestOpen, Title: input.Desired.Title, Body: input.Desired.Body, RemoteHead: input.Desired.Head}
	g.prs = append(g.prs, pr)
	g.drafts = append(g.drafts, input.Draft)
	return forge.PullRequestObservation{Found: true, PullRequest: pr}, nil
}
func (g *fakeGitHub) Update(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	pr := &g.prs[input.ExistingPR.Number-34901]
	pr.Title, pr.Body = input.Desired.Title, input.Desired.Body
	return forge.PullRequestObservation{Found: true, PullRequest: *pr}, nil
}
func (g *fakeGitHub) Inspect(context.Context, record.PullRequestRef) (record.PullRequestStatus, error) {
	return g.status, nil
}

func (g *fakeGitHub) OpenPullRequests(context.Context, string, string) ([]forge.PullRequestSummary, error) {
	return []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}, nil
}

func withGitHub(t *testing.T, w world) *fakeGitHub {
	fork := filepath.Join(filepath.Dir(w.upstream), "fork.git")
	gitRun(t, filepath.Dir(w.upstream), "clone", "-q", "--bare", w.upstream, fork)
	gitRun(t, w.clone, "remote", "add", "fork", fork)
	g := &fakeGitHub{upstream: w.upstream, fork: fork}
	testForge = func(*engine.Engine) engine.Forge { return g }
	t.Cleanup(func() { testForge = nil })
	return g
}

func TestSubmitPreviewsThenOpensThePullRequest(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "src", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	_, _, err = dockhand(t, "submit", "--no-check")
	require.ErrorContains(t, err, "these edits are not committed: textproc/jq/Portfile. Commit them with dockhand tidy")
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)

	out, _, err := dockhand(t, "submit")
	require.ErrorContains(t, err, "nothing was submitted")
	require.Contains(t, out, "jq-update · can't submit yet\n")
	require.Contains(t, out, "✗ no check has finished for this commit's files")

	out, _, err = dockhand(t, "submit", "--no-check")
	require.ErrorContains(t, err, "--yes submits exactly what is shown")
	require.Contains(t, out, "jq-update · ready to submit\n"+
		"  Title    jq: update to 1.8.1\n"+
		"  From     ada/macports-ports:dockhand/jq-update\n"+
		"  To       macports/macports-ports:master\n"+
		"  Commits  1, follows MacPorts' commit rules\n")
	require.Contains(t, out, "  Checks   none (--no-check); the pull request says MacPorts CI is its only check\n")
	require.Contains(t, out, "  Other PRs  #34777 jq: update to 1.8.0\n")
	require.Contains(t, out, "  PR       opens a new one\n")
	require.Empty(t, g.prs)

	var stdout, errs bytes.Buffer
	err = Run(t.Context(), []string{"submit", "--draft"}, Streams{In: strings.NewReader("y\n\np\ns\n"), Out: &stdout, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Contains(t, errs.String(), "? Did you test the basic functionality of all binary files? [y/N] ")
	require.Contains(t, stdout.String(), "Opened draft #34901  https://github.com/macports/macports-ports/pull/34901\n")
	require.Len(t, g.prs, 1)
	require.True(t, g.drafts[0])
	body := g.prs[0].Body
	require.Contains(t, body, "- [x] tested basic functionality of all binary files?")
	require.Contains(t, body, "- [ ] checked that the Portfile's most important [variants]")
	require.Contains(t, body, "- [ ] checked that there aren't other open [pull requests](https://github.com/macports/macports-ports/pulls) for the same change? (open for the same ports: #34777)")
	require.Contains(t, stdout.String(), body, "the preview showed the description")
	require.Equal(t, gitRun(t, dir, "rev-parse", "HEAD"), gitRun(t, g.fork, "rev-parse", "dockhand/jq-update"))
}

func TestStatusRefreshShowsWhatTheReviewersSaid(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "src", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)

	g.status = record.PullRequestStatus{Review: "changes-requested", ChangesRequested: 1, Checks: record.CheckSummary{Total: 2, Passed: 2}}
	t.Setenv("MACPORTS_TREE", w.clone)
	out, errs, err := dockhand(t, "status", "--refresh")
	require.NoError(t, err)
	require.Contains(t, errs, "jq-update: #34901: changes requested\n")
	require.Contains(t, out, "Needs you\n  ! jq-update  #34901 changes requested (just now)  dockhand edit jq\n")
	require.Contains(t, out, "#34901 changes requested, CI ✓")

	// serve reads them by itself.
	g.status = record.PullRequestStatus{Review: "none", Checks: record.CheckSummary{Total: 2, Passed: 1, Failed: 1, Failing: []string{"macOS 26"}}}
	poll := servePoll
	t.Cleanup(func() { servePoll = poll })
	servePoll = 20 * time.Millisecond
	ctx, stop := context.WithCancel(t.Context())
	var served syncBuffer
	done := make(chan error)
	go func() {
		done <- Run(ctx, []string{"serve"}, Streams{In: strings.NewReader(""), Out: &served, Err: &served})
	}()
	require.Eventually(t, func() bool { return strings.Contains(served.String(), "jq-update: #34901: CI failing\n") }, 5*time.Second, 10*time.Millisecond)
	stop()
	require.NoError(t, <-done)
	out, _, err = dockhand(t, "status", "--attention")
	require.Equal(t, 3, ExitCode(err))
	require.Contains(t, out, "✗ jq-update  #34901 MacPorts CI failing: macOS 26")
}
