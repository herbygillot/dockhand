package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	readied        []int
	reviews        []forge.ReviewInput
	rerequested    []string
	// theirs are other people's pull requests, by number.
	theirs map[int]record.PullRequest
	status record.PullRequestStatus
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
	if pr, ok := g.theirs[ref.Number]; ok {
		return forge.PullRequestObservation{Found: true, PullRequest: pr}, nil
	}
	pr := g.prs[ref.Number-34901]
	// GitHub reports the head the fork's branch is at, whoever pushed it.
	if out, err := exec.Command("git", "-C", g.fork, "for-each-ref", "--format=%(objectname)", "refs/heads/"+pr.HeadBranch).Output(); err == nil && len(bytes.TrimSpace(out)) > 0 {
		pr.RemoteHead = record.ObjectID(bytes.TrimSpace(out))
	}
	return forge.PullRequestObservation{Found: true, PullRequest: pr}, nil
}
func (g *fakeGitHub) Create(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	number := 34901 + len(g.prs)
	pr := record.PullRequest{Ref: record.PullRequestRef{Repository: input.Repository, Number: number, URL: fmt.Sprintf("https://github.com/%s/pull/%d", input.Repository, number)},
		HeadBranch: input.HeadBranch, State: record.PullRequestOpen, Title: input.Desired.Title, Body: input.Desired.Body, RemoteHead: input.Desired.Head}
	g.prs = append(g.prs, pr)
	g.drafts = append(g.drafts, input.Draft)
	return forge.PullRequestObservation{Found: true, PullRequest: pr}, nil
}
func (g *fakeGitHub) Update(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	pr := &g.prs[input.ExistingPR.Number-34901]
	pr.Title, pr.Body, pr.RemoteHead = input.Desired.Title, input.Desired.Body, input.Desired.Head
	return forge.PullRequestObservation{Found: true, PullRequest: *pr}, nil
}
func (g *fakeGitHub) MarkReady(_ context.Context, ref record.PullRequestRef) (forge.PullRequestObservation, error) {
	g.readied = append(g.readied, ref.Number)
	return g.Observe(context.Background(), ref)
}

func (g *fakeGitHub) Permission(context.Context, string, string) (string, error) {
	return "read", nil
}

func (g *fakeGitHub) PostReview(_ context.Context, input forge.ReviewInput) (string, error) {
	g.reviews = append(g.reviews, input)
	return fmt.Sprintf("https://github.com/%s/pull/%d#pullrequestreview-%d", input.Ref.Repository, input.Ref.Number, len(g.reviews)), nil
}

func (g *fakeGitHub) RequestReviewers(_ context.Context, _ record.PullRequestRef, logins []string) error {
	g.rerequested = append(g.rerequested, logins...)
	return nil
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

func TestCleanAfterTheMerge(t *testing.T) {
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
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	g.prs[0].State = record.PullRequestMerged
	t.Setenv("MACPORTS_TREE", w.clone)
	_, errs, err := dockhand(t, "status", "--refresh")
	require.NoError(t, err)
	require.Contains(t, errs, "jq-update: #34901 is merged")

	out, _, err := dockhand(t, "clean", "--merged")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update (#34901, merged at ")
	require.Contains(t, out, "  remove   worktree ~/src/macports-branches/jq-update\n  remove   branch dockhand/jq-update\n  remove   ada/macports-ports:dockhand/jq-update\nNothing was removed; --yes removes these.\n")
	require.DirExists(t, dir)

	out, _, err = dockhand(t, "clean", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "  removed  worktree ~/src/macports-branches/jq-update\n")
	require.NoDirExists(t, dir)
	out, _, err = dockhand(t, "clean")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out)
}

func TestSubmitCheckPassingAndReady(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	withScript(t, w, "failed")
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "src", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)

	out, _, err := dockhand(t, "submit", "--check")
	require.Equal(t, 2, ExitCode(err))
	require.ErrorContains(t, err, "; nothing was submitted")
	require.Contains(t, out, "  Checks   runs now; submit follows only if it passes (--check)\n")
	require.Contains(t, out, "jq-update · checking commit ")
	require.Empty(t, g.prs, "a failed check submits nothing")

	withScript(t, w, "passed")
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)
	out, _, err = dockhand(t, "submit", "--passing")
	require.ErrorContains(t, err, "--passing asks about each branch, so it needs a terminal")
	require.Contains(t, out, "1 branch passed its check\n  jq-update\n")

	var stdout, errs bytes.Buffer
	err = Run(t.Context(), []string{"submit", "--passing"}, Streams{In: strings.NewReader("y\nn\nd\ny\n"), Out: &stdout, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "For every branch submitted now:\n")
	require.Contains(t, stdout.String(), "jq-update  jq: update to 1.8.1 · passed on command")
	require.Contains(t, stdout.String(), "+version 1.8.1", "d showed the diff")
	require.Contains(t, stdout.String(), "Opened #34901")
	require.Contains(t, stdout.String(), "Submitted 1 of 1.\n")
	require.Contains(t, g.prs[0].Body, "- [x] tested basic functionality of all binary files?")
	require.Contains(t, g.prs[0].Body, "- [ ] checked that the Portfile's most important [variants]")

	out, _, err = dockhand(t, "submit", "--passing")
	require.NoError(t, err)
	require.Equal(t, "0 branches passed their checks\n", out, "a pushed branch is done")

	out, _, err = dockhand(t, "submit", "--ready", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "Marked #34901 ready for review.\n")
	require.Equal(t, []int{34901}, g.readied)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n# reviewed\n"), 0o644))
	gitRun(t, dir, "commit", "-q", "-am", "jq: note the review")
	out, _, err = dockhand(t, "submit", "--check")
	require.NoError(t, err)
	require.Contains(t, out, "Passed for commit ")
	require.Contains(t, out, "Updated #34901: pushed up to ")
	require.Equal(t, gitRun(t, dir, "rev-parse", "HEAD"), gitRun(t, g.fork, "rev-parse", "dockhand/jq-update"))
}

func TestSubmitAsksTheReviewersBack(t *testing.T) {
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
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	g.status = record.PullRequestStatus{Review: "changes-requested", ChangesRequested: 1, ChangesRequestedBy: []string{"ryandesign"}}
	_, _, err = dockhand(t, "status", "--refresh")
	require.NoError(t, err)

	fix := func(line string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n"+line+"\n"), 0o644))
		gitRun(t, dir, "commit", "-q", "-am", "jq: "+line)
	}
	fix("# drop the patch")
	out, _, err := dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err, out)
	require.Contains(t, out, "@ryandesign requested changes; ask them to review again on GitHub, or set submit.rerequest_review = \"always\"\n")
	require.Empty(t, g.rerequested)

	fix("# and the docs")
	var stdout, errs bytes.Buffer
	err = Run(t.Context(), []string{"submit", "--no-check", "--tested-binaries"}, Streams{In: strings.NewReader("s\n\n"), Out: &stdout, Err: &errs, interactive: true})
	require.NoError(t, err, stdout.String())
	require.Contains(t, errs.String(), "? ask @ryandesign to review again? [Y/n] ")
	require.Contains(t, stdout.String(), "Asked @ryandesign to review again.\n")
	require.Equal(t, []string{"ryandesign"}, g.rerequested)

	require.NoError(t, os.MkdirAll(filepath.Join(w.home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".dockhand", "config.toml"), []byte("[submit]\nrerequest_review = \"never\"\n"), 0o644))
	fix("# once more")
	out, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.NotContains(t, out, "ryandesign")
	require.Equal(t, []string{"ryandesign"}, g.rerequested, "never asks")
}

func TestAdoptRecognizesARenamedBranch(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withGitHub(t, w)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "src", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)

	gitRun(t, dir, "branch", "-m", "jq-1.8")
	t.Setenv("MACPORTS_TREE", w.clone)
	out, _, err := dockhand(t, "status", "--attention")
	require.Equal(t, 3, ExitCode(err))
	require.Contains(t, out, "! jq-update  its Git branch is gone  dockhand adopt <new name>, if you renamed it")

	t.Setenv("MACPORTS_TREE", dir)
	out, _, err = dockhand(t, "adopt")
	require.NoError(t, err)
	require.Equal(t, "Recognized jq-1.8 as dockhand/jq-update, renamed with Git: its record, checks, and history carry over.\n#34901's head can't move, so submit keeps pushing to ada/macports-ports:dockhand/jq-update.\n", out)
	out, _, err = dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "#34901")
}
