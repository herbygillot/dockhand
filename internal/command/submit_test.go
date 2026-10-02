package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/scratch"
)

// fakeGitHub stands in for GitHub: the fork is a local bare repository.
type fakeGitHub struct {
	upstream, fork string
	prs            []forge.PullRequest
	drafts         []bool
	readied        []int
	reviews        []forge.ReviewInput
	rerequested    []string
	// theirs are other people's pull requests, by number.
	theirs map[int]forge.PullRequest
	status forge.PullRequestStatus
	// others are the open pull requests a search for a port finds, and
	// searchErr why the search fails. The first quiet searches find
	// nothing, as before another was opened; searches counts them all.
	others    []forge.PullRequestSummary
	searchErr error
	quiet     int
	searches  int
	// readyRefused is GitHub's refusal to mark a pull request ready.
	readyRefused error
	// dockhand are the commits of dockhand's own repository GitHub has,
	// and dockhandErr why asking it fails.
	dockhand    []string
	dockhandErr error
}

func (g *fakeGitHub) AuthenticatedUser(context.Context) (string, error) { return "ada", nil }

func (g *fakeGitHub) Repository(_, name string) (forge.Repository, error) {
	return fakeDockhand{g: g, name: name}, nil
}

// fakeDockhand is dockhand's own repository on GitHub, which has no tags.
type fakeDockhand struct {
	g    *fakeGitHub
	name string
}

func (d fakeDockhand) Name() string { return d.name }
func (d fakeDockhand) Tag(context.Context, string) (forge.Tag, error) {
	return forge.Tag{}, forge.ErrNotFound
}
func (d fakeDockhand) ListTags(context.Context) ([]forge.Tag, error) { return nil, nil }
func (d fakeDockhand) HasCommit(_ context.Context, commit string) (bool, error) {
	if d.g.dockhandErr != nil {
		return false, d.g.dockhandErr
	}
	return slices.ContainsFunc(d.g.dockhand, func(c string) bool { return strings.HasPrefix(c, commit) }), nil
}
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
func (g *fakeGitHub) Observe(_ context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error) {
	if pr, ok := g.theirs[ref.Number]; ok {
		return forge.PullRequestObservation{Found: true, PullRequest: pr}, nil
	}
	pr := g.prs[ref.Number-34901]
	// GitHub reports the head the fork's branch is at, whoever pushed it.
	if out, err := exec.Command("git", "-C", g.fork, "for-each-ref", "--format=%(objectname)", "refs/heads/"+pr.HeadBranch).Output(); err == nil && len(bytes.TrimSpace(out)) > 0 {
		pr.RemoteHead = model.ObjectID(bytes.TrimSpace(out))
	}
	return forge.PullRequestObservation{Found: true, PullRequest: pr}, nil
}
func (g *fakeGitHub) Create(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	number := 34901 + len(g.prs)
	pr := forge.PullRequest{Ref: forge.PullRequestRef{Repository: input.Repository, Number: number, URL: fmt.Sprintf("https://github.com/%s/pull/%d", input.Repository, number)},
		HeadBranch: input.HeadBranch, State: forge.PullRequestOpen, Title: input.Desired.Title, Body: input.Desired.Body, RemoteHead: input.Desired.Head}
	g.prs = append(g.prs, pr)
	g.drafts = append(g.drafts, input.Draft)
	return forge.PullRequestObservation{Found: true, PullRequest: pr}, nil
}
func (g *fakeGitHub) Update(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	pr := &g.prs[input.ExistingPR.Number-34901]
	pr.Title, pr.Body, pr.RemoteHead = input.Desired.Title, input.Desired.Body, input.Desired.Head
	return forge.PullRequestObservation{Found: true, PullRequest: *pr}, nil
}
func (g *fakeGitHub) MarkReady(_ context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error) {
	if g.readyRefused != nil {
		return forge.PullRequestObservation{}, g.readyRefused
	}
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

func (g *fakeGitHub) RequestReviewers(_ context.Context, _ forge.PullRequestRef, logins []string) error {
	g.rerequested = append(g.rerequested, logins...)
	return nil
}

func (g *fakeGitHub) Inspect(context.Context, forge.PullRequestRef) (forge.PullRequestStatus, error) {
	return g.status, nil
}

func (g *fakeGitHub) OpenPullRequests(context.Context, string, string) ([]forge.PullRequestSummary, error) {
	g.searches++
	if g.searches <= g.quiet {
		return nil, nil
	}
	return g.others, g.searchErr
}

func withGitHub(t *testing.T, w world) *fakeGitHub {
	fork := filepath.Join(filepath.Dir(w.upstream), "fork.git")
	gitRun(t, filepath.Dir(w.upstream), "clone", "-q", "--bare", w.upstream, fork)
	gitRun(t, w.clone, "remote", "add", "fork", fork)
	g := &fakeGitHub{upstream: w.upstream, fork: fork, others: []forge.PullRequestSummary{{Number: 34777, Title: "jq: update to 1.8.0"}}}
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
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
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

	// --plan is the preview on its own: it succeeds, and pushes and
	// opens nothing.
	out, _, err = dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ready to submit\n")
	require.Contains(t, out, "Nothing was submitted (--plan).\n")
	require.Empty(t, g.prs)
	require.Empty(t, gitRun(t, g.fork, "branch", "--list", "dockhand/jq-update"), "nothing was pushed")
	_, _, err = dockhand(t, "submit", "--plan", "--check")
	require.ErrorContains(t, err, "--plan previews one branch's submission")

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

// The preview says what comparing the upstream archives found, as update
// did and submit --passing does, and its JSON carries it: a person's own
// submission shows it, and isn't held for it (D4).
func TestTheSubmitPreviewSaysWhatUpstreamFound(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	testPreparer = func(e *engine.Engine) engine.Preparer { return unfetchedPrevious{bumper{repo: e.Repo}} }
	t.Cleanup(func() { testPreparer = nil })
	withGitHub(t, w)
	started, err := jsonOf(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", dig(t, started.Result, "branch", "worktree").(string))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)

	out, _, err := dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ready to submit\n")
	require.Contains(t, out, "  Upstream ! archives not compared: the current version's archives could not be fetched: HTTP 404\n")
	preview, err := jsonOf(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Equal(t, []any{map[string]any{"port": "jq", "changes": []any{}, "held": true,
		"problem": "the current version's archives could not be fetched: HTTP 404"}}, preview.Result["upstream"])
}

// Each update's findings get a line, and name their port when the branch
// updated several; an update whose archives showed nothing to look at
// says so.
func TestTheSubmitPreviewGivesEachUpstreamFindingALine(t *testing.T) {
	license := model.UpstreamChange{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed", Hold: true}
	dropped := model.UpstreamChange{Kind: "dependency", Path: "go.mod", Message: "upstream: go.mod drops golang.org/x/net"}
	preview := func(upstream ...engine.PortComparison) string {
		var out bytes.Buffer
		writeSubmitPlan(&out, engine.SubmitPlan{Branch: model.Branch{Name: "dockhand/jq-update"}, Upstream: upstream})
		return out.String()
	}

	require.NotContains(t, preview(), "Upstream", "a branch whose updates compared no archives")
	require.Contains(t, preview(engine.PortComparison{Port: "jq", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{}}}),
		"\n  Upstream compared; no license, build file, or dependency changes\n  Other PRs")
	require.Contains(t, preview(engine.PortComparison{Port: "jq", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{license, dropped}}}),
		"\n  Upstream ! LICENSE changed\n"+
			"           · go.mod drops golang.org/x/net\n"+
			"           (! holds bump's and serve's submission for your look; · holds nothing)\n  Other PRs", "the label says upstream once")
	require.Contains(t, preview(engine.PortComparison{Port: "jq", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{license}}},
		engine.PortComparison{Port: "oniguruma6", Comparison: model.UpstreamComparison{Problem: "HTTP 404"}},
		engine.PortComparison{Port: "jq", Comparison: model.UpstreamComparison{}}),
		"\n  Upstream ! jq: LICENSE changed\n"+
			"           ! oniguruma6: archives not compared: HTTP 404\n"+
			"           (! holds bump's and serve's submission for your look; · holds nothing)\n  Other PRs",
		"an update with nothing to look at adds no line among others' findings")
}

// Under an Upstream heading, a finding doesn't say upstream again; where
// no heading says whose it is it would keep the word, and every list of
// them has one, --passing's included (the ov run's finding 5).
func TestUpstreamIsSaidOnceUnderItsHeading(t *testing.T) {
	license := model.UpstreamChange{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed", Hold: true}
	dropped := model.UpstreamChange{Kind: "dependency", Path: "go.mod", Message: "upstream: go.mod drops golang.org/x/net"}
	var update bytes.Buffer
	writeUpstream(&update, &model.UpstreamComparison{Changes: []model.UpstreamChange{license, dropped}})
	require.Equal(t, "Upstream changes:\n  ! LICENSE changed\n  · go.mod drops golang.org/x/net\n  (! holds bump's and serve's submission for your look; · holds nothing)\n", update.String(), "a list with a mark that holds says what the marks mean (the chezmoi run)")
	update.Reset()
	writeUpstream(&update, &model.UpstreamComparison{Changes: []model.UpstreamChange{dropped}})
	require.Equal(t, "Upstream changes:\n  · go.mod drops golang.org/x/net\n", update.String(), "one without needs no legend")
	plan := engine.SubmitPlan{Upstream: []engine.PortComparison{{Port: "jq", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{license, dropped}}}}}
	require.Equal(t, []string{"! LICENSE changed", "· go.mod drops golang.org/x/net"}, upstreamLines(plan), "every list of them has a heading now, --passing's too")
}

// A commit whose Generated-By names a dockhand built from uncommitted
// source is shown before it's submitted, since nobody else can find that
// build; the preview's JSON lists it. One naming a committed build isn't.
func TestSubmitShowsACommitNamingAModifiedBuild(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withGitHub(t, w)
	started, err := jsonOf(t, "start", "jq-update")
	require.NoError(t, err)
	dir := dig(t, started.Result, "branch", "worktree").(string)
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	trailer := "\n\nGenerated-By: Dockhand v0.0.0-20260924.0.0.20260928175309-2bbcfdb76480%s (https://github.com/herbygillot/dockhand)"
	gitRun(t, dir, "commit", "-q", "-am", "jq: update to 1.8.1"+fmt.Sprintf(trailer, ""))
	out, _, err := dockhand(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.NotContains(t, out, "uncommitted source")

	gitRun(t, dir, "commit", "-q", "--amend", "-m", "jq: update to 1.8.1"+fmt.Sprintf(trailer, "+dirty"))
	out, _, err = dockhand(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Contains(t, out, "  ! commit "+engine.Short(model.ObjectID(gitRun(t, dir, "rev-parse", "HEAD")))+"'s Generated-By names a dockhand built from uncommitted source, which nobody else can find; tidy it again with a build of a pushed commit\n")
	preview, err := jsonOf(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Equal(t, []any{gitRun(t, dir, "rev-parse", "HEAD")}, preview.Result["modified_builds"])
}

// A commit whose Generated-By names a build dockhand's repository on
// GitHub doesn't have is shown before it's submitted, as one naming a
// build of uncommitted source is, since nobody else can find that build
// either (the hugo exercise, finding 1); the preview's JSON lists it
// beside modified_builds. Where GitHub can't be asked, that's said once.
// Neither stops the submission.
func TestSubmitSaysABuildGitHubDoesntHave(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	started, err := jsonOf(t, "start", "jq-update")
	require.NoError(t, err)
	dir := dig(t, started.Result, "branch", "worktree").(string)
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	unpushed := "v0.0.0-20260924.0.0.20260928190000-14320eb7c0de"
	gitRun(t, dir, "commit", "-q", "-am", "jq: update to 1.8.1\n\nGenerated-By: Dockhand "+unpushed+" (https://github.com/herbygillot/dockhand)")
	head := gitRun(t, dir, "rev-parse", "HEAD")

	out, _, err := dockhand(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ready to submit\n")
	require.Contains(t, out, "  ! commit "+engine.Short(model.ObjectID(head))+"'s Generated-By names dockhand "+unpushed+", built from a commit dockhand's GitHub repository doesn't have, which nobody else can find until it's pushed there\n")
	preview, err := jsonOf(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Equal(t, []any{map[string]any{"build": unpushed, "commits": []any{head}, "commit": "14320eb7c0de"}}, preview.Result["unfound_builds"])
	require.NotContains(t, preview.Result, "builds_problem")

	g.dockhandErr = errors.New("github: API rate limit exceeded")
	out, _, err = dockhand(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Contains(t, out, "  ! couldn't ask GitHub whether it has the dockhand builds the commits name in Generated-By: github: API rate limit exceeded\n")
	require.NotContains(t, out, "pushed there")
	preview, err = jsonOf(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Equal(t, []any{}, preview.Result["unfound_builds"])
	require.Equal(t, "github: API rate limit exceeded", preview.Result["builds_problem"])

	g.dockhandErr, g.dockhand = nil, []string{"14320eb7c0de" + strings.Repeat("0", 28)}
	out, _, err = dockhand(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.NotContains(t, out, "Generated-By", "a build GitHub has is said nowhere")

	g.dockhand = nil
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.Len(t, g.prs, 1, "nothing here stops the submission")
}

// A build several commits name is said once, naming each; one built at a
// release's tag says the tag, and one that recorded no commit says so.
func TestAnUnfoundBuildIsSaidOnceForItsCommits(t *testing.T) {
	commits := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
	require.Equal(t, "commits aaaaaaa, bbbbbbb name dockhand v0.3.0 in Generated-By, built at a tag dockhand's GitHub repository doesn't have, which nobody else can find until it's pushed there",
		unfoundBuildWords(engine.UnfoundBuild{Build: "v0.3.0", Commits: commits, Source: buildinfo.Source{Release: "v0.3.0"}}))
	require.Equal(t, "commit aaaaaaa's Generated-By names dockhand devel, which recorded no commit, so nobody can find what it was built from",
		unfoundBuildWords(engine.UnfoundBuild{Build: "devel", Commits: commits[:1]}))
}

// A tidy whose commits name a dockhand built from uncommitted source says
// so as it writes them, with how to name one anybody can find.
func TestTidyWarnsOfAModifiedBuild(t *testing.T) {
	plan := func(tag string) engine.TidyPlan {
		return engine.TidyPlan{Groups: []engine.TidyGroup{{Message: "jq: update to 1.8.1\n\nGenerated-By: Dockhand " + tag + " (https://github.com/herbygillot/dockhand)"}}}
	}
	require.Equal(t, "! Generated-By names this dockhand, built from uncommitted source, which nobody else can find. Before submitting, tidy again with a build of a pushed commit: dockhand restore tidy-3, then dockhand tidy.",
		modifiedBuildWarning(plan("devel+1a2b3c4d5e6f.modified"), "tidy-3"))
	require.Empty(t, modifiedBuildWarning(plan("devel+1a2b3c4d5e6f"), "tidy-3"))
}

// Someone else's pull request keeps its title and description, whatever
// submit would write, and the preview says so.
func TestAPullRequestOfSomeoneElsesStaysTheirs(t *testing.T) {
	plan := engine.SubmitPlan{Existing: &forge.PullRequestObservation{PullRequest: forge.PullRequest{Ref: forge.PullRequestRef{Number: 34905}}}, Theirs: true, Sections: engine.DescriptionSections{Types: engine.SectionRefreshed}}
	require.Equal(t, "updates #34905; its title and description are theirs, and stay as they are", pullRequestWords(plan))
	plan.Theirs = false
	plan.BodyKept = true
	require.Equal(t, "updates #34905; refreshes its Type(s); the rest of its description is yours", pullRequestWords(plan), "types a person names, on a description they edited")
}

// A commit's body written after the pull request opened reaches its
// Description while that is still dockhand's, and the preview says so
// (the hugo exercise's re-submitting sshuttle, finding 3).
func TestAResubmitRefreshesTheDescriptionItWrote(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.NotContains(t, g.prs[0].Body, "Built against the new libonig")

	subject, rest, _ := strings.Cut(gitRun(t, dir, "log", "-1", "--format=%B"), "\n")
	gitRun(t, dir, "commit", "-q", "--amend", "-m", subject+"\n\nBuilt against the new libonig.\n"+rest)
	out, _, err := dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; refreshes its Description section\n`, out)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.Contains(t, g.prs[0].Body, "#### Description\n\nBuilt against the new libonig.\n\n###### Type(s)")

	edited := strings.Replace(g.prs[0].Body, "Built against the new libonig.", "What I tested by hand.", 1)
	g.prs[0].Body = edited
	out, _, err = dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; its description is yours, and stays as it is\n`, out, "a Description a person edited is theirs")
}

// Every part refreshed is named, in the description's order.
func TestTheRefreshedPartsAreNamed(t *testing.T) {
	plan := engine.SubmitPlan{Existing: &forge.PullRequestObservation{PullRequest: forge.PullRequest{Ref: forge.PullRequestRef{Number: 34905}}},
		Sections: engine.DescriptionSections{Description: engine.SectionRefreshed, Types: engine.SectionRefreshed, TestedOn: engine.SectionRefreshed}}
	require.Equal(t, "updates #34905; refreshes its Description section, its Type(s) and its description from Tested on down", pullRequestWords(plan))
	plan.Sections.Types = engine.SectionCurrent
	require.Equal(t, "updates #34905; refreshes its Description section and its description from Tested on down", pullRequestWords(plan))
}

// submit --note gives a person's note under the description's
// Description, kept with the branch: submitting again without it keeps
// it, a new one replaces it, which is the description's update, and
// --note "" takes it out. The preview shows it, and says when a
// Description a person edited won't give it. A note is one branch's, so
// --passing takes none. dockhand writes the whole description, so a
// person had no way to say in it why rust's tests failed (the rust run,
// #35084).
func TestSubmitNoteIsKeptWithTheBranch(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	started, err := jsonOf(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", dig(t, started.Result, "branch", "worktree").(string))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)

	note := "The tests failed on a permission error,\nbefore any test ran."
	out, _, err := dockhand(t, "submit", "--no-check", "--plan", "--note", note)
	require.NoError(t, err)
	require.Contains(t, out, "  Note     The tests failed on a permission error, …\n")
	require.Contains(t, out, "    > **Author's note:** The tests failed on a permission error,\n    > before any test ran.\n")
	_, _, err = dockhand(t, "submit", "--no-check", "--yes", "--note", note)
	require.NoError(t, err)
	require.Contains(t, g.prs[0].Body, "#### Description\n\n> **Author's note:** The tests failed on a permission error,\n> before any test ran.\n\n###### Type(s)")

	out, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "#34901 has nothing new: the fork has its commit, and its title and description are current\n", "the note is kept")
	require.Contains(t, g.prs[0].Body, "> **Author's note:** The tests failed")

	out, _, err = dockhand(t, "submit", "--no-check", "--yes", "--note", "Fixed upstream.")
	require.NoError(t, err)
	require.Contains(t, out, "Updated #34901's description\n")
	require.Contains(t, g.prs[0].Body, "#### Description\n\n> **Author's note:** Fixed upstream.\n\n###### Type(s)")
	require.NotContains(t, g.prs[0].Body, "permission error")

	out, _, err = dockhand(t, "submit", "--no-check", "--yes", "--note", "")
	require.NoError(t, err)
	require.Contains(t, out, "Updated #34901's description\n")
	require.NotContains(t, g.prs[0].Body, "Author's note")
	out, _, err = dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.NotContains(t, out, "  Note ")

	g.prs[0].Body = strings.Replace(g.prs[0].Body, "#### Description\n\n", "#### Description\n\nWhat I tested by hand.\n\n", 1)
	out, _, err = dockhand(t, "submit", "--no-check", "--plan", "--note", "Fixed upstream.")
	require.NoError(t, err)
	require.Contains(t, out, "  Note     Fixed upstream.\n           ! not in the description: its Description is yours, edited on GitHub or taken out, and stays as it is\n")

	_, _, err = dockhand(t, "submit", "--passing", "--note", "Fixed upstream.")
	require.ErrorContains(t, err, "none of the others can be")
}

// Submitting to a pull request with nothing to push says what it changed:
// the title, the description, or nothing at all.
func TestSubmitSaysWhatItUpdated(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withGitHub(t, w)
	started, err := jsonOf(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", dig(t, started.Result, "branch", "worktree").(string))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)

	out, _, err := dockhand(t, "submit", "--no-check", "--yes", "--title", "jq: update to 1.8.1, reviewed")
	require.NoError(t, err)
	require.Contains(t, out, "Updated #34901's title\n")
	out, _, err = dockhand(t, "submit", "--no-check", "--yes", "--type", "bugfix")
	require.NoError(t, err)
	require.Contains(t, out, "Updated #34901's description\n")
	out, _, err = dockhand(t, "submit", "--no-check", "--yes", "--type", "enhancement", "--title", "jq: update to 1.8.1")
	require.NoError(t, err)
	require.Contains(t, out, "Updated #34901's title and description\n")
	out, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "#34901 has nothing new: the fork has its commit, and its title and description are current\n")
}

func TestStatusRefreshShowsWhatTheReviewersSaid(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	out, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  PR       #34901 · CI not read yet (dockhand status --refresh)\n", "until something reads it, status says how")

	g.status = forge.PullRequestStatus{Review: "changes-requested", ChangesRequested: 1, Checks: forge.CheckSummary{Total: 2, Passed: 2}}
	t.Setenv("MACPORTS_TREE", w.clone)
	out, errs, err := dockhand(t, "status", "--refresh")
	require.NoError(t, err)
	require.Contains(t, errs, "jq-update: #34901: changes requested\n")
	require.Contains(t, out, "Needs you\n  ! jq-update  #34901 changes requested (just now)  dockhand edit jq\n")
	require.Contains(t, out, "#34901 changes requested, CI ✓")
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	out, _, err = dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  PR       #34901 changes requested, CI ✓\n", "once read, it says what it read")
	t.Setenv("MACPORTS_TREE", w.clone)

	// serve reads them by itself.
	g.status = forge.PullRequestStatus{Review: "none", Checks: forge.CheckSummary{Total: 2, Passed: 1, Failed: 1, Failing: []string{"macOS 26"}}}
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
	require.Contains(t, out, "open https://github.com/macports/macports-ports/pull/34901/checks")
}

func TestCleanAfterTheMerge(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	g.prs[0].State = forge.PullRequestMerged
	t.Setenv("MACPORTS_TREE", w.clone)
	_, errs, err := dockhand(t, "status", "--refresh")
	require.NoError(t, err)
	require.Contains(t, errs, "jq-update: #34901 is merged")

	out, _, err := dockhand(t, "clean", "--merged")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update (#34901, merged at ")
	require.Contains(t, out, "  remove   worktree ~/Source/macports-branches/jq-update\n  remove   branch dockhand/jq-update\n  remove   ada/macports-ports:dockhand/jq-update\nNothing was removed; --yes removes these.\n")
	require.DirExists(t, dir)

	out, _, err = dockhand(t, "clean", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "  removed  worktree ~/Source/macports-branches/jq-update\n")
	require.NoDirExists(t, dir)
	out, _, err = dockhand(t, "clean")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out)

	// What clean removed on purpose needs no one, and the record it keeps
	// is found by name as status --all lists it (the hugo exercise's
	// "Cleaning up", findings 1 and 2).
	out, _, err = dockhand(t, "status", "--all")
	require.NoError(t, err)
	require.NotContains(t, out, "Needs you")
	require.Regexp(t, `jq-update +— +cleaned +— +#34901 merged\n`, out)
	require.NotContains(t, out, "not pushed")
	all, err := jsonOf(t, "status", "--all")
	require.NoError(t, err)
	require.Empty(t, all.Result["attention"])
	require.Equal(t, true, dig(t, all.Result, "branches", 0, "cleaned"))
	out, _, err = dockhand(t, "status", "jq-update")
	require.NoError(t, err)
	require.Equal(t, "jq-update\n  Work     cleaned after its merge\n  PR       #34901 merged\n", out, "its worktree, ports, and checks went with it, and nothing is next")
	_, _, err = dockhand(t, "update", "jq", "--branch", "jq-update")
	require.ErrorContains(t, err, "no tracked branch named jq-update", "a merged branch takes no changes")
	_, _, err = dockhand(t, "start", "jq-update")
	require.NoError(t, err, "a merged branch's name can be used again")
	out, _, err = dockhand(t, "status", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ~/Source/macports-branches/jq-update\n  Ports    none yet\n")
	require.Contains(t, out, "  PR       —\n", "the open branch of the name, not the merged record")
}

func TestSubmitCheckPassingAndReady(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	testPreparer = func(e *engine.Engine) engine.Preparer { return unfetchedPrevious{bumper{repo: e.Repo}} }
	t.Cleanup(func() { testPreparer = nil })
	g := withGitHub(t, w)
	withScript(t, w, "failed")
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
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
	// Nor does --yes stand in for the look at each, which it had been
	// ignored for (D11, from the ov run's finding 7).
	out, _, err = dockhand(t, "submit", "--passing", "--yes")
	require.EqualError(t, err, "--passing asks about each branch, so it takes no --yes; dockhand submit --branch <name> --yes submits one without asking")
	require.Empty(t, out, "refused before anything is looked at")

	var stdout, errs bytes.Buffer
	err = Run(t.Context(), []string{"submit", "--passing"}, Streams{In: strings.NewReader("y\nn\nd\ny\n"), Out: &stdout, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "For every branch submitted now:\n")
	require.Contains(t, stdout.String(), "jq-update  jq: update to 1.8.1 · passed on command")
	require.Contains(t, stdout.String(), "\n            Upstream ! archives not compared: the current version's archives could not be fetched: HTTP 404\n",
		"what upstream showed, which holds only a submission nobody looks over")
	require.Contains(t, stdout.String(), "+version 1.8.1", "d showed the diff")
	require.Contains(t, stdout.String(), "Opened #34901")
	require.Contains(t, stdout.String(), "Submitted 1 of 1.\n")
	require.Contains(t, g.prs[0].Body, "- [x] tested basic functionality of all binary files?")
	require.Contains(t, g.prs[0].Body, "- [ ] checked that the Portfile's most important [variants]")

	out, _, err = dockhand(t, "submit", "--passing")
	require.NoError(t, err)
	require.Equal(t, "0 branches passed their checks\n", out, "a pushed branch is done")

	// --ready is previewed, as the step that changes the pull request's
	// state, and a draft's preview names it; the preview shows the
	// description, which only its JSON had (the sshuttle run, and the
	// hugo exercise).
	previewed, _, err := dockhand(t, "submit", "--plan", "--ready")
	require.NoError(t, err)
	require.Contains(t, previewed, "#34901 isn't a draft, so --ready has nothing to do.\n")
	require.Contains(t, previewed, "\nDescription, as the pull request would have it:\n    Submitted by **[dockhand](https://github.com/herbygillot/dockhand)**\n\n    #### Description\n")
	require.Contains(t, previewed, "Nothing was submitted (--plan).\n")
	require.Empty(t, g.readied, "a preview marks nothing")

	// GitHub may refuse dockhand's app, as an organization restricting
	// apps does: what to do instead is said (the sshuttle run, finding 2),
	// and the GitHub CLI does it where it's signed in as dockhand is (D8).
	g.readyRefused = restricted{errors.New("github: the `macports` organization has enabled OAuth App access restrictions")}
	refused := "marking #34901 ready for review: github: the `macports` organization has enabled OAuth App access restrictions\n" +
		"It's still a draft. Mark it ready on its page, https://github.com/macports/macports-ports/pull/34901, or with the GitHub CLI, which signs in as its own app: gh pr ready 34901 --repo macports/macports-ports\n"
	_, _, err = dockhand(t, "submit", "--ready", "--yes")
	require.EqualError(t, err, refused+"The GitHub CLI wasn't used: the GitHub CLI isn't installed.")
	var byCLI []int
	gitHubCLI = signedIn{login: "bob", readied: &byCLI}
	t.Cleanup(func() { gitHubCLI = signedIn{} })
	_, _, err = dockhand(t, "submit", "--ready", "--yes")
	require.EqualError(t, err, refused+"The GitHub CLI wasn't used: it's signed in as bob, and dockhand as ada.")
	require.Empty(t, byCLI, "another account's CLI isn't used")
	gitHubCLI = signedIn{login: "Ada", readied: &byCLI}
	out, _, err = dockhand(t, "submit", "--ready", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "Marked #34901 ready for review with the GitHub CLI: the macports organization refuses dockhand's app.\n")
	require.Equal(t, []int{34901}, byCLI)
	require.Empty(t, g.readied)
	g.readyRefused = errors.New("github: Could not resolve to a node")
	_, _, err = dockhand(t, "submit", "--ready", "--yes")
	require.ErrorContains(t, err, "github: Could not resolve to a node\nIt's still a draft.")
	require.NotContains(t, err.Error(), "wasn't used", "a refusal that isn't the organization's isn't the CLI's to try")
	require.Equal(t, []int{34901}, byCLI)
	g.readyRefused = nil

	out, _, err = dockhand(t, "submit", "--ready", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "Marked #34901 ready for review.\n")
	require.Equal(t, []int{34901}, g.readied)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n# reviewed\n"), 0o644))
	gitRun(t, dir, "commit", "-q", "-am", "jq: note the review")
	out, _, err = dockhand(t, "submit", "--check")
	require.NoError(t, err)
	require.Contains(t, out, "Passed for commit ")
	require.NotContains(t, out, "Next: ", "submit --check submits; it doesn't send you to submit")
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
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	// The preview says whether submitting again changes the description.
	out, _, err := dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; its description is current\n`, out)
	out, _, err = dockhand(t, "submit", "--no-check", "--plan", "--tested-binaries")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; refreshes its description from Tested on down\n`, out)
	// Its Type(s) are dockhand's too while unchanged, and --type names them
	// on a pull request already open, as on a new one.
	out, _, err = dockhand(t, "submit", "--no-check", "--plan", "--type", "bugfix")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; refreshes its Type\(s\)\n`, out)
	typed, err := jsonOf(t, "submit", "--no-check", "--plan", "--type", "bugfix")
	require.NoError(t, err)
	require.Contains(t, typed.Result["body"], "- [x] bugfix\n- [ ] enhancement\n")
	out, _, err = dockhand(t, "submit", "--no-check", "--plan", "--type", "enhancement")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; its description is current\n`, out, "an update is already an enhancement")
	// Type(s) a person ticked on GitHub stay theirs, unless --type names others.
	opened := g.prs[0].Body
	g.prs[0].Body = strings.Replace(opened, "- [ ] bugfix", "- [x] bugfix", 1)
	out, _, err = dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; its description is current\n`, out, "the person's tick stays")
	typed, err = jsonOf(t, "submit", "--no-check", "--plan", "--type", "security fix")
	require.NoError(t, err)
	require.Contains(t, typed.Result["body"], "- [ ] bugfix\n- [ ] enhancement\n- [x] security fix\n")
	g.prs[0].Body = opened
	g.status = forge.PullRequestStatus{Review: "changes-requested", ChangesRequested: 1, ChangesRequestedBy: []string{"ryandesign"}}
	_, _, err = dockhand(t, "status", "--refresh")
	require.NoError(t, err)

	fix := func(line string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n"+line+"\n"), 0o644))
		gitRun(t, dir, "commit", "-q", "-am", "jq: "+line)
	}
	fix("# drop the patch")
	out, _, err = dockhand(t, "submit", "--no-check", "--yes")
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
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
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
	require.Contains(t, out, "! jq-update  its Git branch is gone, and its pull request, #34901, may have merged  dockhand status --refresh reads it; dockhand adopt <new name>, if you renamed the branch",
		"with a pull request, either may be so")

	t.Setenv("MACPORTS_TREE", dir)
	out, _, err = dockhand(t, "adopt")
	require.NoError(t, err)
	require.Equal(t, "Recognized jq-1.8 as dockhand/jq-update, renamed with Git: its record, checks, and history carry over.\n#34901's head can't move, so submit keeps pushing to ada/macports-ports:dockhand/jq-update.\n", out)
	out, _, err = dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "#34901")
}

// A branch has one check at a time: submit --check doesn't queue a second
// beside one already queued for the same commit, and says how to finish.
// And it submits exactly the commit it checked: a branch that moved while
// its check ran is not submitted.
func TestSubmitCheckKeepsToOneCheckAndItsCommit(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	withScript(t, w, "passed")
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)

	_, _, err = dockhand(t, "check", "--head", "--enqueue")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--check")
	require.EqualError(t, err, "check-1 is already queued for these files; dockhand wait check-1 follows it; once it passes, dockhand submit --branch jq-update submits this commit")
	_, _, err = dockhand(t, "cancel", "check-1")
	require.NoError(t, err)

	// The build commits to the branch as it runs.
	script := filepath.Join(w.home, "bin", "build-ports")
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
git -C "$MOVE_IN" commit -q --allow-empty -m "moved while checked"
cat > "$(dirname "$1")/result.json" <<JSON
{"version": 1, "targets": [{"id": "jq", "outcome": "passed"}]}
JSON
`), 0o755))
	t.Setenv("MOVE_IN", dir)
	_, _, err = dockhand(t, "submit", "--check")
	require.Error(t, err)
	require.Regexp(t, `^jq-update moved to [0-9a-f]{7} while it was checked; nothing was submitted, since submit --check binds [0-9a-f]{7}$`, err.Error())
	require.Empty(t, g.prs)
}

// The description is edited in the process's run root, so a process that
// dies with the editor open leaves the buffer to the next one's sweep.
func TestTheDescriptionIsEditedInTheRunRoot(t *testing.T) {
	record := filepath.Join(t.TempDir(), "path")
	t.Setenv("VISUAL", `edit() { printf '%s' "$1" > '`+record+`'; printf 'edited' > "$1"; }; edit`)
	text, err := editText(t.Context(), "draft")
	require.NoError(t, err)
	require.Equal(t, "edited", text)
	path, err := os.ReadFile(record)
	require.NoError(t, err)
	root, err := scratch.Root()
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(path), root+string(filepath.Separator)), "%s is under %s", path, root)
	require.NoFileExists(t, string(path), "the buffer goes when the edit is done")
}

// submit's plan says the check passed only where it built something, and
// names where every port was excluded, rather than calling it tested there
// (the beekeeper-studio run's finding 4).
func TestSubmitsCheckLineNamesOnlyWhereItBuilt(t *testing.T) {
	monterey := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	target := model.PlanTarget{ID: "beekeeper-studio", Target: model.Target{Name: "beekeeper-studio"}}
	evidence := &engine.Evidence{
		Run: model.Run{ID: "run_21", Number: 21},
		Plan: model.Plan{Environments: []model.Environment{monterey, tahoe}, Builds: []model.EnvironmentPlan{
			{Environment: monterey, Exclusions: []model.Exclusion{{Target: target.Target, Reason: "known_fail"}}},
			{Environment: tahoe, Order: []model.TargetID{target.ID}},
		}},
		Targets:    []engine.TargetEvidence{{Target: target, Passed: true, Outcomes: cells([]model.TargetResult{{Outcome: model.OutcomeNotRun}, {Execution: "tart_b", Outcome: model.OutcomePassed}})}},
		Executions: map[model.ExecutionID]model.GuestExecution{"tart_b": {ID: "tart_b", Run: "run_21"}},
	}
	require.Equal(t, "passed on "+engine.DescribeEnvironment(tahoe)+" for this commit's files (check-21); nothing built on "+engine.DescribeEnvironment(monterey)+", where every port is excluded",
		checkWords(engine.SubmitPlan{Evidence: evidence}))
}

// restricted is GitHub's refusal of an app an organization hasn't
// approved, as the forge's client reports it.
type restricted struct{ error }

func (restricted) Is(target error) bool { return target == forge.ErrAppRestricted }

// A draft's preview names the command that marks it ready, and --ready's
// preview says what it would do in each case.
func TestTheReadyStepIsPreviewed(t *testing.T) {
	draft := engine.SubmitPlan{Branch: model.Branch{PullRequest: &model.PullRequest{Number: 34901, Draft: true}},
		Existing: &forge.PullRequestObservation{PullRequest: forge.PullRequest{Ref: forge.PullRequestRef{Number: 34901}}}}
	require.True(t, strings.HasPrefix(pullRequestWords(draft), "updates #34901, a draft: dockhand submit --ready marks it ready for review"), pullRequestWords(draft))
	for _, test := range []struct {
		plan  engine.SubmitPlan
		ready bool
		words string
	}{
		{draft, true, "Then marks #34901 ready for review.\n"},
		{engine.SubmitPlan{Branch: model.Branch{PullRequest: &model.PullRequest{Number: 34901}}}, true, "#34901 isn't a draft, so --ready has nothing to do.\n"},
		{engine.SubmitPlan{Request: engine.SubmitRequest{Draft: true}}, true, "Opens it as a draft, then marks it ready for review.\n"},
		{engine.SubmitPlan{}, true, "It opens ready for review, not as a draft, so --ready has nothing to do.\n"},
		{draft, false, ""},
	} {
		var out bytes.Buffer
		writeReadyPreview(&out, test.plan, test.ready)
		require.Equal(t, test.words, out.String())
	}
}

// Naming a branch cleans it alone, as its state has it, and nothing else:
// the sand-runner session's person asked for one merged branch cleaned,
// and clean would have swept every merged one (batch 31). An open branch
// has nothing to clean. A branch cleaned by hand after its merge, before
// status has read the merge, says its pull request may have merged, where
// it suggested adopt as if something were wrong.
func TestCleanNamesOneBranch(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", w.clone)
	_, _, err = dockhand(t, "start", "other-work")
	require.NoError(t, err)
	_, _, err = dockhand(t, "archive", "other-work")
	require.NoError(t, err)

	// Cleaned by hand, before status knows of the merge.
	gitRun(t, w.clone, "worktree", "remove", "--force", dir)
	gitRun(t, w.clone, "branch", "-D", "dockhand/jq-update")
	out, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "its Git branch is gone, and its pull request, #34901, may have merged")
	require.Contains(t, out, "dockhand status --refresh reads it")

	g.prs[0].State = forge.PullRequestMerged
	_, _, err = dockhand(t, "status", "--refresh")
	require.NoError(t, err)
	_, _, err = dockhand(t, "clean", "other-work-missing")
	require.ErrorContains(t, err, "no tracked branch named other-work-missing")
	_, _, err = dockhand(t, "start", "open-work")
	require.NoError(t, err)
	_, _, err = dockhand(t, "clean", "open-work")
	require.ErrorContains(t, err, "open-work is open, so there's nothing to clean")

	out, _, err = dockhand(t, "clean", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update (#34901, merged at ")
	require.NotContains(t, out, "other-work", "an archived branch not named is left")
	require.Contains(t, out, "  remove   ada/macports-ports:dockhand/jq-update\n")
	out, _, err = dockhand(t, "clean", "jq-update", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "  removed  ada/macports-ports:dockhand/jq-update\n")
	require.DirExists(t, filepath.Join(w.home, "Source", "macports-branches", "other-work"), "the archived branch's worktree stays")
}
