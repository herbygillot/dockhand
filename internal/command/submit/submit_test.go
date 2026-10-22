package submit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestSubmitPreviewsThenOpensThePullRequest(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)

	// Edits not committed are committed first, as tidy would, then
	// submitted, where submit refused and named tidy (Design v3 §3's
	// chaining); --plan shows the commits and makes none.
	planned, _, err := cmdtest.Dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Contains(t, planned, "jq-update · commits first, edits not yet committed\n")
	require.Contains(t, planned, "  1  jq: update to 1.8.1\n")
	require.Contains(t, planned, "Nothing was committed or submitted (--plan).")
	require.NotEmpty(t, testsupport.Git(t, dir, "status", "--porcelain"), "nothing committed")
	committed, _, err := cmdtest.Dockhand(t, "submit", "--no-check")
	require.ErrorContains(t, err, "nothing was submitted: without a terminal, --yes submits exactly what is shown")
	require.Contains(t, committed, "Created 1 commit.")
	require.Contains(t, committed, "jq-update · ready to submit\n")
	require.Equal(t, "jq: update to 1.8.1", strings.TrimSpace(testsupport.Git(t, dir, "log", "-1", "--format=%s")))

	out, _, err := cmdtest.Dockhand(t, "submit")
	require.ErrorContains(t, err, "nothing was submitted")
	require.Contains(t, out, "jq-update · can't submit yet\n")
	require.Contains(t, out, "✗ no check has finished for this commit's files")

	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check")
	require.ErrorContains(t, err, "--yes submits exactly what is shown")
	require.Contains(t, out, "jq-update · ready to submit\n"+
		"  Title    jq: update to 1.8.1\n"+
		"  From     ada/macports-ports:dockhand/jq-update\n"+
		"  As       ada, from GH_TOKEN\n"+
		"  To       macports/macports-ports:master\n"+
		"  Commits  1, follows MacPorts' commit rules\n")
	require.Contains(t, out, "  Checks   none (--no-check); the pull request says MacPorts CI is its only check\n")
	require.Contains(t, out, "  Other PRs  #34777 jq: update to 1.8.0\n")
	require.Contains(t, out, "  PR       opens a new one\n")
	require.Empty(t, g.PRs)

	// --plan is the preview on its own: it succeeds, and pushes and
	// opens nothing.
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ready to submit\n")
	require.Contains(t, out, "Nothing was submitted (--plan).\n")
	require.Empty(t, g.PRs)
	require.Empty(t, testsupport.Git(t, g.Fork, "branch", "--list", "dockhand/jq-update"), "nothing was pushed")

	// The acceptance test's sandbox takes the pull requests within itself,
	// and the preview says it isn't MacPorts'.
	cmdtest.Setenv(t, "DOCKHAND_PULL_REQUESTS", "ada/macports-ports")
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "  To       ada/macports-ports:master\n           a sandbox, not macports/macports-ports, as DOCKHAND_PULL_REQUESTS says\n")
	cmdtest.Setenv(t, "DOCKHAND_PULL_REQUESTS", "")
	// The JSON names the account, where its token comes from, and whose
	// the fork is (the rc9 full stage's D-C4).
	named, err := cmdtest.JSONOf(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Equal(t, "ada", named.Result["account"])
	require.Equal(t, "GH_TOKEN", named.Result["login_source"])
	require.Equal(t, "ada", named.Result["fork_owner"])
	_, _, err = cmdtest.Dockhand(t, "submit", "--plan", "--yes")
	require.EqualError(t, err, "--plan previews the submission; it goes without --yes")

	var stdout, errs bytes.Buffer
	err = cmdtest.RunFor(t, t.Context(), []string{"submit", "--draft"}, term.Streams{In: strings.NewReader("y\n\np\ns\n"), Out: &stdout, Err: &errs}.AsTerminal())
	require.NoError(t, err)
	require.Contains(t, errs.String(), "? Did you test the basic functionality of all binary files? [y/N] ")
	require.Contains(t, stdout.String(), "Opened draft #34901  https://github.com/macports/macports-ports/pull/34901\n")
	require.Len(t, g.PRs, 1)
	require.True(t, g.Drafts[0])
	body := g.PRs[34901].Body
	require.Contains(t, body, "- [x] tested basic functionality of all binary files?")
	require.Contains(t, body, "- [ ] checked that the Portfile's most important [variants]")
	require.Contains(t, body, "- [ ] checked that there aren't other open [pull requests](https://github.com/macports/macports-ports/pulls) for the same change? (open for the same ports: #34777)")
	require.Contains(t, stdout.String(), body, "the preview showed the description")
	require.Equal(t, testsupport.Git(t, dir, "rev-parse", "HEAD"), testsupport.Git(t, g.Fork, "rev-parse", "dockhand/jq-update"))
}

// The preview says what comparing the upstream archives found, as update
// did and ship --passing does, and its JSON carries it: a person's own
// submission shows it, and isn't held for it (D4).
func TestTheSubmitPreviewSaysWhatUpstreamFound(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer {
		return cmdtest.UnfetchedPrevious{Bumper: cmdtest.Bumper{Repo: e.Repo}}
	}
	cmdtest.WithGitHub(t, w)
	started, err := cmdtest.JSONOf(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", cmdtest.Dig(t, started.Result, "branch", "worktree").(string))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)

	out, _, err := cmdtest.Dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ready to submit\n")
	require.Contains(t, out, "  Upstream ! held: archives not compared: the current version's archives could not be fetched: HTTP 404\n")
	preview, err := cmdtest.JSONOf(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Equal(t, []any{map[string]any{"port": "jq", "changes": []any{}, "held": true,
		"problem": "the current version's archives could not be fetched: HTTP 404"}}, preview.Result["upstream"])
}

// Each update's findings get a line, and name their port when the branch
// updated several; an update whose archives showed nothing to look at
// says so.
func TestTheSubmitPreviewGivesEachUpstreamFindingALine(t *testing.T) {
	t.Parallel()
	license := model.UpstreamChange{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed", Hold: true}
	dropped := model.UpstreamChange{Kind: "dependency", Path: "go.mod", Message: "upstream: go.mod drops golang.org/x/net"}
	preview := func(upstream ...engine.PortComparison) string {
		var out bytes.Buffer
		view.WriteSubmitPlan(&out, engine.SubmitPlan{Branch: model.Branch{Name: "dockhand/jq-update"}, Upstream: upstream}, true)
		return out.String()
	}

	require.NotContains(t, preview(), "Upstream", "a branch whose updates compared no archives")
	require.Contains(t, preview(engine.PortComparison{Port: "jq", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{}}}),
		"\n  Upstream compared; no license, build file, or dependency changes\n  Other PRs")
	require.Contains(t, preview(engine.PortComparison{Port: "jq", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{license, dropped}}}),
		"\n  Upstream ! held: LICENSE changed\n"+
			"             go.mod drops golang.org/x/net\n  Other PRs", "the label says upstream once; a hold says so in its word")
	require.Contains(t, preview(engine.PortComparison{Port: "jq", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{license}}},
		engine.PortComparison{Port: "oniguruma6", Comparison: model.UpstreamComparison{Problem: "HTTP 404"}},
		engine.PortComparison{Port: "jq", Comparison: model.UpstreamComparison{}}),
		"\n  Upstream ! held: jq: LICENSE changed\n"+
			"           ! held: oniguruma6: archives not compared: HTTP 404\n  Other PRs",
		"an update with nothing to look at adds no line among others' findings")
}

// Under an Upstream heading, a finding doesn't say upstream again; where
// no heading says whose it is it would keep the word, and every list of
// them has one, --passing's included (the ov run's finding 5).
func TestUpstreamIsSaidOnceUnderItsHeading(t *testing.T) {
	t.Parallel()
	license := model.UpstreamChange{Kind: "license", Path: "LICENSE", Message: "upstream's LICENSE changed", Hold: true}
	dropped := model.UpstreamChange{Kind: "dependency", Path: "go.mod", Message: "upstream: go.mod drops golang.org/x/net"}
	var update bytes.Buffer
	view.WriteUpstream(&update, &model.UpstreamComparison{Changes: []model.UpstreamChange{license, dropped}})
	require.Equal(t, "Upstream changes:\n  ! held: LICENSE changed\n    go.mod drops golang.org/x/net\n", update.String(), "a hold says so in its word, where a legend said what the marks meant (the chezmoi run; part 5, item 2)")
	update.Reset()
	view.WriteUpstream(&update, &model.UpstreamComparison{Changes: []model.UpstreamChange{dropped}})
	require.Equal(t, "Upstream changes:\n    go.mod drops golang.org/x/net\n", update.String(), "a finding that holds nothing carries no mark")
	plan := engine.SubmitPlan{Upstream: []engine.PortComparison{{Port: "jq", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{license, dropped}}}}}
	require.Equal(t, []string{"! held: LICENSE changed", "  go.mod drops golang.org/x/net"}, view.UpstreamLines(plan), "every list of them has a heading now, --passing's too")
}

// A commit whose Generated-By names a dockhand built from uncommitted
// source is shown before it's submitted, since nobody else can find that
// build; the preview's JSON lists it. One naming a committed build isn't.
func TestSubmitShowsACommitNamingAModifiedBuild(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithGitHub(t, w)
	started, err := cmdtest.JSONOf(t, "start", "jq-update")
	require.NoError(t, err)
	dir := cmdtest.Dig(t, started.Result, "branch", "worktree").(string)
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	trailer := "\n\nGenerated-By: Dockhand v0.0.0-20260924.0.0.20260928175309-2bbcfdb76480%s (https://github.com/herbygillot/dockhand)"
	testsupport.Git(t, dir, "commit", "-q", "-am", "jq: update to 1.8.1"+fmt.Sprintf(trailer, ""))
	out, _, err := cmdtest.Dockhand(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.NotContains(t, out, "uncommitted source")

	testsupport.Git(t, dir, "commit", "-q", "--amend", "-m", "jq: update to 1.8.1"+fmt.Sprintf(trailer, "+dirty"))
	out, _, err = cmdtest.Dockhand(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Contains(t, out, "  ! Generated-By names a dockhand build from uncommitted source in commit "+model.ObjectID(testsupport.Git(t, dir, "rev-parse", "HEAD")).Short()+". Run tidy again using a build from a pushed commit so others can identify the source.\n")
	preview, err := cmdtest.JSONOf(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Equal(t, []any{testsupport.Git(t, dir, "rev-parse", "HEAD")}, preview.Result["modified_builds"])
}

// A commit whose Generated-By names a build dockhand's repository on
// GitHub doesn't have is shown before it's submitted, as one naming a
// build of uncommitted source is, since nobody else can find that build
// either (the hugo exercise, finding 1); the preview's JSON lists it
// beside modified_builds. Where GitHub can't be asked, that's said once.
// Neither stops the submission.
func TestSubmitSaysABuildGitHubDoesntHave(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	started, err := cmdtest.JSONOf(t, "start", "jq-update")
	require.NoError(t, err)
	dir := cmdtest.Dig(t, started.Result, "branch", "worktree").(string)
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	unpushed := "v0.0.0-20260924.0.0.20260928190000-14320eb7c0de"
	testsupport.Git(t, dir, "commit", "-q", "-am", "jq: update to 1.8.1\n\nGenerated-By: Dockhand "+unpushed+" (https://github.com/herbygillot/dockhand)")
	head := testsupport.Git(t, dir, "rev-parse", "HEAD")

	out, _, err := cmdtest.Dockhand(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ready to submit\n")
	require.Contains(t, out, "  ! commit "+model.ObjectID(head).Short()+"'s Generated-By names dockhand "+unpushed+", built from a commit dockhand's GitHub repository doesn't have, which nobody else can find until it's pushed there\n")
	preview, err := cmdtest.JSONOf(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Equal(t, []any{map[string]any{"build": unpushed, "commits": []any{head}, "commit": "14320eb7c0de"}}, preview.Result["unfound_builds"])
	require.NotContains(t, preview.Result, "builds_problem")

	g.Dockhand.Err = errors.New("github: API rate limit exceeded")
	out, _, err = cmdtest.Dockhand(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Contains(t, out, "  ! Could not verify the Dockhand builds listed in Generated-By on GitHub: github: API rate limit exceeded\n")
	require.NotContains(t, out, "pushed there")
	preview, err = cmdtest.JSONOf(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.Equal(t, []any{}, preview.Result["unfound_builds"])
	require.Equal(t, "github: API rate limit exceeded", preview.Result["builds_problem"])

	g.Dockhand.Err, g.Dockhand.Commits = nil, []string{"14320eb7c0de" + strings.Repeat("0", 28)}
	out, _, err = cmdtest.Dockhand(t, "submit", "--plan", "--no-check")
	require.NoError(t, err)
	require.NotContains(t, out, "Generated-By", "a build GitHub has is said nowhere")

	g.Dockhand.Commits = nil
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.Len(t, g.PRs, 1, "nothing here stops the submission")
}

// A build several commits name is said once, naming each; one built at a
// release's tag says the tag, and one that recorded no commit says so.
func TestAnUnfoundBuildIsSaidOnceForItsCommits(t *testing.T) {
	t.Parallel()
	commits := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
	require.Equal(t, "commits aaaaaaa, bbbbbbb name dockhand v0.3.0 in Generated-By, built at a tag dockhand's GitHub repository doesn't have, which nobody else can find until it's pushed there",
		view.UnfoundBuildWords(engine.UnfoundBuild{Build: "v0.3.0", Commits: commits, Source: buildinfo.Source{Release: "v0.3.0"}}))
	require.Equal(t, "commit aaaaaaa's Generated-By names dockhand devel, which recorded no commit, so nobody can find what it was built from",
		view.UnfoundBuildWords(engine.UnfoundBuild{Build: "devel", Commits: commits[:1]}))
}

// A tidy whose commits name a dockhand built from uncommitted source says
// so as it writes them, with how to name one anybody can find.
func TestTidyWarnsOfAModifiedBuild(t *testing.T) {
	t.Parallel()
	plan := func(tag string) engine.TidyPlan {
		return engine.TidyPlan{Groups: []engine.TidyGroup{{Message: "jq: update to 1.8.1\n\nGenerated-By: Dockhand " + tag + " (https://github.com/herbygillot/dockhand)"}}}
	}
	require.Equal(t, "! Generated-By names this dockhand, built from uncommitted source, which nobody else can find. Before submitting, tidy again with a build of a pushed commit: dockhand undo tidy-3, then dockhand tidy.",
		view.ModifiedBuildWarning(plan("devel+1a2b3c4d5e6f.modified"), "tidy-3"))
	require.Empty(t, view.ModifiedBuildWarning(plan("devel+1a2b3c4d5e6f"), "tidy-3"))
}

// Someone else's pull request keeps its title and description, whatever
// submit would write, and the preview says so.
func TestAPullRequestOfSomeoneElsesStaysTheirs(t *testing.T) {
	t.Parallel()
	plan := engine.SubmitPlan{Existing: &forge.PullRequestObservation{PullRequest: forge.PullRequest{Ref: forge.PullRequestRef{Number: 34905}}}, Theirs: true, Sections: engine.DescriptionSections{Types: engine.SectionRefreshed}}
	require.Equal(t, "updates #34905; its title and description are theirs, and stay as they are", view.PullRequestWords(plan))
	plan.Theirs = false
	plan.BodyKept = true
	require.Equal(t, "updates #34905; refreshes its Type(s); the rest of its description is yours", view.PullRequestWords(plan), "types a person names, on a description they edited")
}

// A commit's body written after the pull request opened reaches its
// Description while that is still dockhand's, and the preview says so
// (the hugo exercise's re-submitting sshuttle, finding 3).
func TestAResubmitRefreshesTheDescriptionItWrote(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.NotContains(t, g.PRs[34901].Body, "Built against the new libonig")

	subject, rest, _ := strings.Cut(testsupport.Git(t, dir, "log", "-1", "--format=%B"), "\n")
	testsupport.Git(t, dir, "commit", "-q", "--amend", "-m", subject+"\n\nBuilt against the new libonig.\n"+rest)
	out, _, err := cmdtest.Dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; refreshes its Description section\n`, out)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.Contains(t, g.PRs[34901].Body, "#### Description\n\nBuilt against the new libonig.\n\n###### Type(s)")

	edited := strings.Replace(g.PRs[34901].Body, "Built against the new libonig.", "What I tested by hand.", 1)
	g.PRs[34901].Body = edited
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; its description is yours, and stays as it is\n`, out, "a Description a person edited is theirs")

	// It stays theirs through every submit after, not the next alone:
	// recorded merged, their text read as dockhand's on the third, which
	// replaced it (the architecture re-synthesis, L1). An un-ticked Type
	// stays un-ticked too.
	unticked := strings.Replace(g.PRs[34901].Body, "- [x] enhancement", "- [ ] enhancement", 1)
	g.PRs[34901].Body = unticked
	for range 2 {
		_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
		require.NoError(t, err)
		require.Contains(t, g.PRs[34901].Body, "What I tested by hand.")
		require.Contains(t, g.PRs[34901].Body, "- [ ] enhancement")
	}

	// --rewrite-description replaces it, a person's own Description
	// included, when they ask: an adopted branch's description, which
	// nothing recorded dockhand writing, kept September's Tested on under
	// the v0.3.0 port's commit (#34756).
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--plan", "--rewrite-description")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; replaces its whole description with dockhand's \(--rewrite-description\)\n`, out)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes", "--rewrite-description")
	require.NoError(t, err)
	require.NotContains(t, g.PRs[34901].Body, "What I tested by hand.")
	require.Contains(t, g.PRs[34901].Body, "#### Description\n\nBuilt against the new libonig.\n\n###### Type(s)")
	require.Contains(t, g.PRs[34901].Body, "- [x] enhancement")
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; its description is current\n`, out, "dockhand's again, once rewritten")
}

// Every part refreshed is named, in the description's order.
func TestTheRefreshedPartsAreNamed(t *testing.T) {
	t.Parallel()
	plan := engine.SubmitPlan{Existing: &forge.PullRequestObservation{PullRequest: forge.PullRequest{Ref: forge.PullRequestRef{Number: 34905}}},
		Sections: engine.DescriptionSections{Description: engine.SectionRefreshed, Types: engine.SectionRefreshed, TestedOn: engine.SectionRefreshed}}
	require.Equal(t, "updates #34905; refreshes its Description section, its Type(s) and its description from Tested on down", view.PullRequestWords(plan))
	plan.Sections.Types = engine.SectionCurrent
	require.Equal(t, "updates #34905; refreshes its Description section and its description from Tested on down", view.PullRequestWords(plan))
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
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	started, err := cmdtest.JSONOf(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", cmdtest.Dig(t, started.Result, "branch", "worktree").(string))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)

	note := "The tests failed on a permission error,\nbefore any test ran."
	out, _, err := cmdtest.Dockhand(t, "submit", "--no-check", "--plan", "--note", note)
	require.NoError(t, err)
	require.Contains(t, out, "  Note     The tests failed on a permission error, …\n")
	require.Contains(t, out, "    The tests failed on a permission error,\n    before any test ran.\n")
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes", "--note", note)
	require.NoError(t, err)
	require.Contains(t, g.PRs[34901].Body, "#### Description\n\nThe tests failed on a permission error,\nbefore any test ran.\n\n###### Type(s)")

	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "#34901 has nothing new: the fork has its commit, and its title and description are current\n", "the note is kept")
	// and says so, since it outlives the commits it was written beside
	// (field testing, grafana 13.2.4).
	require.Contains(t, out, "  Note     The tests failed on a permission error, …\n           kept from an earlier submit, across rewritten commits; --note \"\" clears it\n")
	require.Contains(t, g.PRs[34901].Body, "#### Description\n\nThe tests failed")

	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes", "--note", "Fixed upstream.")
	require.NoError(t, err)
	require.Contains(t, out, "Updated #34901's description\n")
	require.Contains(t, g.PRs[34901].Body, "#### Description\n\nFixed upstream.\n\n###### Type(s)")
	require.NotContains(t, g.PRs[34901].Body, "permission error")

	updated, err := cmdtest.JSONOf(t, "submit", "--no-check", "--yes", "--note", "")
	require.NoError(t, err)
	pr, _ := updated.Result["pull_request"].(map[string]any)
	require.Equal(t, true, pr["updated_description"], "the JSON says what was rewritten")
	require.Equal(t, false, pr["updated_title"])
	require.NotContains(t, g.PRs[34901].Body, "Author's note")
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "  Note     missing; the commit has no body, so the description does not explain the change. Add --note \"…\".\n",
		"with the note taken out, nothing says what changed (the Vx port's field testing)")

	g.PRs[34901].Body = strings.Replace(g.PRs[34901].Body, "#### Description\n\n", "#### Description\n\nWhat I tested by hand.\n\n", 1)
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--plan", "--note", "Fixed upstream.")
	require.NoError(t, err)
	require.Contains(t, out, "  Note     Fixed upstream.\n           ! note omitted: the description was edited or removed on GitHub, so Dockhand preserves it\n")
}

// Submitting to a pull request with nothing to push says what it changed:
// the title, the description, or nothing at all.
func TestSubmitSaysWhatItUpdated(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithGitHub(t, w)
	started, err := cmdtest.JSONOf(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", cmdtest.Dig(t, started.Result, "branch", "worktree").(string))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)

	out, _, err := cmdtest.Dockhand(t, "submit", "--no-check", "--yes", "--title", "jq: update to 1.8.1, reviewed")
	require.NoError(t, err)
	require.Contains(t, out, "Updated #34901's title\n")
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes", "--type", "bugfix")
	require.NoError(t, err)
	require.Contains(t, out, "Updated #34901's description\n")
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes", "--type", "enhancement", "--title", "jq: update to 1.8.1")
	require.NoError(t, err)
	require.Contains(t, out, "Updated #34901's title and description\n")
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "#34901 has nothing new: the fork has its commit, and its title and description are current\n")
}

func TestStatusRefreshShowsWhatTheReviewersSaid(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	// status reads a pull request it has no reading of, or an old one,
	// itself (SQL-O8); one it can't read now is said as last read, and
	// status goes on.
	g.ObserveErr = errors.New("dial tcp: connection refused")
	out, errs, err := cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  PR       #34901 · CI not read yet (dockhand status --refresh)\n", "until something reads it, status says how")
	require.Contains(t, errs, "Could not refresh #34901; showing previously recorded status: dial tcp: connection refused\n")
	g.ObserveErr = nil
	out, errs, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, errs, "jq-update: #34901 is open\n")
	require.Contains(t, out, "  PR       #34901\n", "read now")

	g.Status = forge.PullRequestStatus{Review: "changes-requested", ChangesRequested: 1, Checks: forge.CheckSummary{Total: 2, Passed: 2}}
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	out, errs, err = cmdtest.Dockhand(t, "status", "--refresh")
	require.NoError(t, err)
	require.Contains(t, errs, "jq-update: #34901: changes requested\n")
	require.Contains(t, out, "Needs you\n  ! jq-update  #34901 changes requested (just now)\n               dockhand edit jq\n")
	require.Contains(t, out, "#34901 changes requested, CI ✓")
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	out, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  PR       #34901 changes requested, CI ✓\n", "once read, it says what it read")
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)

	// serve reads them by itself.
	g.Status = forge.PullRequestStatus{Review: "none", Checks: forge.CheckSummary{Total: 2, Passed: 1, Failed: 1, Failing: []string{"macOS 26"}}}
	cmdtest.Hosted(t).Timings.ServePoll = 20 * time.Millisecond
	ctx, stop := context.WithCancel(t.Context())
	var served cmdtest.SyncBuffer
	done := make(chan error)
	go func() {
		done <- cmdtest.RunFor(t, ctx, []string{"serve"}, term.Streams{In: strings.NewReader(""), Out: &served, Err: &served})
	}()
	require.Eventually(t, func() bool { return strings.Contains(served.String(), "jq-update: #34901: CI failing\n") }, cmdtest.Settle, 10*time.Millisecond)
	stop()
	require.NoError(t, <-done)
	out, _, err = cmdtest.Dockhand(t, "status", "--attention")
	require.Equal(t, 3, term.ExitCode(err))
	require.Contains(t, out, "✗ jq-update  #34901 MacPorts CI failing: macOS 26")
	require.Contains(t, out, "open https://github.com/macports/macports-ports/pull/34901/checks")

	// Its failed job's log is read, once, and the failure said by what made
	// it, with the failed jobs run again as its Next (garage #35174, field
	// testing, 2026-10-03).
	g.Failed = map[string][]forge.CIJob{string(g.ForkHead("dockhand/jq-update")): {{ID: 11, Run: 7, Attempt: 1, Name: "macOS 26"}}}
	g.Logs = map[int64]string{11: "DEBUG: Fetching https://mirror.sjtu.edu.cn/macports/distfiles/jq/jq-1.8.1.tar.gz failed: The requested URL returned error: 403\n" +
		"Error: Failed to fetch jq-1.8.1.tar.gz: The requested URL returned error: 403\n"}
	out, _, err = cmdtest.Dockhand(t, "status", "--attention", "--refresh")
	require.Equal(t, 3, term.ExitCode(err))
	require.Contains(t, out, "  ✗ jq-update  #34901 MacPorts CI failed fetching jq-1.8.1.tar.gz (mirror.sjtu.edu.cn answered 403)\n               on macOS 26 (just now)\n               dockhand retry --ci --branch jq-update\n")
	out, _, err = cmdtest.Dockhand(t, "retry", "--ci", "--branch", "jq-update")
	require.NoError(t, err)
	require.Equal(t, "Asked GitHub to run #34901's failed CI again (1 workflow run); dockhand status --refresh reads how it went.\n", out)
	require.Equal(t, []int64{7}, g.Rerun)
	require.Equal(t, 1, g.LogsRead)
}

func TestCleanAfterTheMerge(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	g.PRs[34901].State = forge.PullRequestMerged
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	_, errs, err := cmdtest.Dockhand(t, "status", "--refresh")
	require.NoError(t, err)
	require.Contains(t, errs, "jq-update: #34901 is merged")

	out, _, err := cmdtest.Dockhand(t, "clean", "--merged")
	require.Equal(t, term.ExitFailure, term.ExitCode(err))
	require.Contains(t, out, "jq-update (#34901, merged at ")
	require.Contains(t, out, "  remove   worktree ~/Source/macports-branches/jq-update\n  remove   branch dockhand/jq-update\n  remove   ada/macports-ports:dockhand/jq-update\n")
	require.DirExists(t, dir)

	out, _, err = cmdtest.Dockhand(t, "clean", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update: removed worktree ~/Source/macports-branches/jq-update, branch dockhand/jq-update, ada/macports-ports:dockhand/jq-update\n")
	require.NoDirExists(t, dir)
	out, _, err = cmdtest.Dockhand(t, "clean")
	require.NoError(t, err)
	require.Equal(t, "Nothing to remove.\n", out)

	// What clean removed on purpose needs no one, and the record it keeps
	// is found by name as status --all lists it (the hugo exercise's
	// "Cleaning up", findings 1 and 2).
	out, _, err = cmdtest.Dockhand(t, "status", "--all")
	require.NoError(t, err)
	require.NotContains(t, out, "Needs you")
	require.Regexp(t, `jq-update +— +cleaned +— +#34901 merged\n`, out)
	require.NotContains(t, out, "not pushed")
	all, err := cmdtest.JSONOf(t, "status", "--all")
	require.NoError(t, err)
	require.Empty(t, all.Result["attention"])
	require.Equal(t, true, cmdtest.Dig(t, all.Result, "branches", 0, "cleaned"))
	out, _, err = cmdtest.Dockhand(t, "status", "jq-update")
	require.NoError(t, err)
	require.Equal(t, "jq-update\n  Work     cleaned after its merge\n  PR       #34901 merged\n", out, "its worktree, ports, and checks went with it, and nothing is next")
	_, _, err = cmdtest.Dockhand(t, "update", "jq", "--branch", "jq-update")
	require.ErrorContains(t, err, "jq-update was merged as #34901 and cleaned; --new=jq-update starts a new branch of the name", "a merged branch takes no changes, nor is it replaced by --branch")
	_, _, err = cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err, "a merged branch's name can be used again")
	out, _, err = cmdtest.Dockhand(t, "status", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update in ~/Source/macports-branches/jq-update\n  Ports    none yet\n")
	require.Contains(t, out, "  PR       —\n", "the open branch of the name, not the merged record")
}

func TestShipPassingAndSubmitReady(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.Stands(t).Preparer = func(e *engine.Engine) engine.Preparer {
		return cmdtest.UnfetchedPrevious{Bumper: cmdtest.Bumper{Repo: e.Repo}}
	}
	g := cmdtest.WithGitHub(t, w)
	cmdtest.WithScript(t, w, "failed")
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)

	// ship checks the committed head first, and submits only what passed.
	out, _, err := cmdtest.Dockhand(t, "ship", "--yes", "-v")
	require.Equal(t, 2, term.ExitCode(err))
	require.ErrorContains(t, err, "; nothing was submitted")
	require.Contains(t, out, "  jq-update  its branch, where it stands, to submit\n             jq: update to 1.8.1 · runs first; it is submitted only if it passes · opens a new one\n")
	require.Contains(t, out, "jq-update · checking commit ")
	require.Empty(t, g.PRs, "a failed check submits nothing")

	cmdtest.WithScript(t, w, "passed")
	_, _, err = cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	// Without a terminal nobody looks, so it holds where serve would.
	out, _, err = cmdtest.Dockhand(t, "ship", "--passing")
	require.ErrorContains(t, err, "jq-update passed its check and waits for your look, so nothing was submitted: the upstream archives couldn't be compared")
	require.Contains(t, out, "  jq-update  its branch, where it stands, to submit\n             jq: update to 1.8.1 · passed on command for this commit's files (check-2) · opens a new one\n")
	require.Contains(t, out, "\n             Upstream ! held: archives not compared: the current version's archives could not be fetched: HTTP 404\n",
		"what upstream showed, in the plan, which holds only a submission nobody looks over")
	require.Empty(t, g.PRs)

	var stdout, stderr bytes.Buffer
	err = cmdtest.RunFor(t, t.Context(), []string{"ship", "--passing"}, term.Streams{In: strings.NewReader("y\ny\nn\n"), Out: &stdout, Err: &stderr}.AsTerminal())
	require.NoError(t, err)
	require.Contains(t, stderr.String(), "? Ship it? [y/N] ")
	require.Contains(t, stdout.String(), "✓ submitted  opened #34901")
	require.Contains(t, g.PRs[34901].Body, "- [x] tested basic functionality of all binary files?")
	require.Contains(t, g.PRs[34901].Body, "- [ ] checked that the Portfile's most important [variants]")

	out, _, err = cmdtest.Dockhand(t, "ship", "--passing")
	require.NoError(t, err)
	require.Equal(t, "Nothing to ship.\n", out, "a pushed branch is done")

	// --ready is previewed, as the step that changes the pull request's
	// state, and a draft's preview names it; the preview shows the
	// description, which only its JSON had (the sshuttle run, and the
	// hugo exercise).
	previewed, _, err := cmdtest.Dockhand(t, "submit", "--plan", "--ready")
	require.NoError(t, err)
	require.Contains(t, previewed, "#34901 isn't a draft, so --ready has nothing to do.\n")
	require.Contains(t, previewed, "\nProposed pull request description:\n    Submitted by **[dockhand](https://github.com/herbygillot/dockhand)**\n\n    #### Description\n")
	require.Regexp(t, `\n    \+\d+ lines \(dockhand submit --plan -v shows it all\)\n`, previewed, "its first 8 lines")
	require.Contains(t, previewed, "Nothing was submitted (--plan).\n")
	require.Empty(t, g.Readied, "a preview marks nothing")

	// GitHub may refuse dockhand's app, as an organization restricting
	// apps does: what to do instead is said (the sshuttle run, finding 2),
	// and the GitHub CLI does it where it's signed in as dockhand is (D8).
	g.ReadyRefused = restricted{errors.New("github: the `macports` organization has enabled OAuth App access restrictions")}
	refused := "marking #34901 ready for review: github: the `macports` organization has enabled OAuth App access restrictions\n" +
		"It's still a draft. Mark it ready on its page, https://github.com/macports/macports-ports/pull/34901, or with the GitHub CLI, which signs in as its own app: gh pr ready 34901 --repo macports/macports-ports\n"
	_, _, err = cmdtest.Dockhand(t, "submit", "--ready", "--yes")
	require.EqualError(t, err, refused+"The GitHub CLI wasn't used: the GitHub CLI isn't installed.")
	var byCLI []int
	cmdtest.Stands(t).GitHubCLI = cmdtest.SignedIn{Account: "bob", Readied: &byCLI}
	t.Cleanup(func() { cmdtest.Stands(t).GitHubCLI = cmdtest.SignedIn{} })
	_, _, err = cmdtest.Dockhand(t, "submit", "--ready", "--yes")
	require.EqualError(t, err, refused+"The GitHub CLI wasn't used: it's signed in as bob, and dockhand as ada.")
	require.Empty(t, byCLI, "another account's CLI isn't used")
	cmdtest.Stands(t).GitHubCLI = cmdtest.SignedIn{Account: "Ada", Readied: &byCLI}
	out, _, err = cmdtest.Dockhand(t, "submit", "--ready", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "Marked #34901 ready for review with the GitHub CLI: the macports organization refuses dockhand's app.\n")
	require.Equal(t, []int{34901}, byCLI)
	require.Empty(t, g.Readied)
	g.ReadyRefused = errors.New("github: Could not resolve to a node")
	_, _, err = cmdtest.Dockhand(t, "submit", "--ready", "--yes")
	require.ErrorContains(t, err, "github: Could not resolve to a node\nIt's still a draft.")
	require.NotContains(t, err.Error(), "wasn't used", "a refusal that isn't the organization's isn't the CLI's to try")
	require.Equal(t, []int{34901}, byCLI)
	g.ReadyRefused = nil

	out, _, err = cmdtest.Dockhand(t, "submit", "--ready", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "Marked #34901 ready for review.\n")
	require.Equal(t, []int{34901}, g.Readied)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n# reviewed\n"), 0o644))
	testsupport.Git(t, dir, "commit", "-q", "-am", "jq: note the review")
	// Nobody looks at a submission --yes makes, and no other pull
	// request for the port holds it here.
	g.Others = nil
	out, _, err = cmdtest.Dockhand(t, "ship", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "  ✓ checked    passed on command\n")
	require.NotContains(t, out, "Next: ", "ship submits; it doesn't send you to submit")
	require.Contains(t, out, "  ✓ submitted  pushed to #34901  ")
	require.Equal(t, testsupport.Git(t, dir, "rev-parse", "HEAD"), testsupport.Git(t, g.Fork, "rev-parse", "dockhand/jq-update"))
}

func TestSubmitAsksTheReviewersBack(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	// The preview says whether submitting again changes the description.
	out, _, err := cmdtest.Dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; its description is current\n`, out)
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--plan", "--tested-binaries")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; refreshes its description from Tested on down\n`, out)
	// Its Type(s) are dockhand's too while unchanged, and --type names them
	// on a pull request already open, as on a new one.
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--plan", "--type", "bugfix")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; refreshes its Type\(s\)\n`, out)
	typed, err := cmdtest.JSONOf(t, "submit", "--no-check", "--plan", "--type", "bugfix")
	require.NoError(t, err)
	require.Contains(t, typed.Result["body"], "- [x] bugfix\n- [ ] enhancement\n")
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--plan", "--type", "enhancement")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; its description is current\n`, out, "an update is already an enhancement")
	// Type(s) a person ticked on GitHub stay theirs, unless --type names others.
	opened := g.PRs[34901].Body
	g.PRs[34901].Body = strings.Replace(opened, "- [ ] bugfix", "- [x] bugfix", 1)
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--plan")
	require.NoError(t, err)
	require.Regexp(t, `  PR       updates #\d+; its description is current\n`, out, "the person's tick stays")
	typed, err = cmdtest.JSONOf(t, "submit", "--no-check", "--plan", "--type", "security fix")
	require.NoError(t, err)
	require.Contains(t, typed.Result["body"], "- [ ] bugfix\n- [ ] enhancement\n- [x] security fix\n")
	g.PRs[34901].Body = opened
	g.Status = forge.PullRequestStatus{Review: "changes-requested", ChangesRequested: 1, ChangesRequestedBy: []string{"ryandesign"}}
	_, _, err = cmdtest.Dockhand(t, "status", "--refresh")
	require.NoError(t, err)

	fix := func(line string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n"+line+"\n"), 0o644))
		testsupport.Git(t, dir, "commit", "-q", "-am", "jq: "+line)
	}
	fix("# drop the patch")
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err, out)
	require.Contains(t, out, "@ryandesign requested changes; ask them to review again on GitHub, or set submit.rerequest_review = \"always\"\n")
	require.Empty(t, g.Rerequested)

	fix("# and the docs")
	var stdout, errs bytes.Buffer
	err = cmdtest.RunFor(t, t.Context(), []string{"submit", "--no-check", "--tested-binaries"}, term.Streams{In: strings.NewReader("s\n\n"), Out: &stdout, Err: &errs}.AsTerminal())
	require.NoError(t, err, stdout.String())
	require.Contains(t, errs.String(), "? ask @ryandesign to review again? [Y/n] ")
	require.Contains(t, stdout.String(), "Asked @ryandesign to review again.\n")
	require.Equal(t, []string{"ryandesign"}, g.Rerequested)

	require.NoError(t, os.MkdirAll(filepath.Join(w.Home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"), []byte("[submit]\nrerequest_review = \"never\"\n"), 0o644))
	fix("# once more")
	out, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.NotContains(t, out, "ryandesign")
	require.Equal(t, []string{"ryandesign"}, g.Rerequested, "never asks")

	// The JSON names who requested changes, and whether each was asked to
	// review again (the rc8 full stage's B4), as the text says it.
	pullRequest := func(env cmdtest.Decoded) map[string]any {
		t.Helper()
		pr, ok := env.Result["pull_request"].(map[string]any)
		require.True(t, ok, "%v", env.Result)
		return pr
	}
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"), []byte("[submit]\nrerequest_review = \"ask\"\n"), 0o644))
	fix("# without a terminal")
	result, err := cmdtest.JSONOf(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.Equal(t, []any{map[string]any{"login": "ryandesign", "rerequested": false}}, pullRequest(result)["changes_requested_by"])
	require.Equal(t, false, pullRequest(result)["updated_title"])
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"), []byte("[submit]\nrerequest_review = \"always\"\n"), 0o644))
	fix("# always")
	result, err = cmdtest.JSONOf(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	require.Equal(t, []any{map[string]any{"login": "ryandesign", "rerequested": true}}, pullRequest(result)["changes_requested_by"])
	require.Equal(t, []string{"ryandesign", "ryandesign"}, g.Rerequested)
}

func TestAdoptRecognizesARenamedBranch(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithGitHub(t, w)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)

	testsupport.Git(t, dir, "branch", "-m", "jq-1.8")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	out, _, err := cmdtest.Dockhand(t, "adopt")
	require.NoError(t, err)
	require.Equal(t, "Recognized jq-1.8 as a Git rename of dockhand/jq-update; retained its record, checks, and history.\nPull request #34901 keeps its original source branch; submit will continue pushing to ada/macports-ports:dockhand/jq-update.\n", out)
	out, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "#34901")
}

// status follows a branch renamed with Git without being asked, and says
// so once (the person's ruling on the M1's quick stage, D-S5).
func TestStatusFollowsARenamedBranch(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	testsupport.Git(t, dir, "branch", "-m", "dockhand/jq-1.8")
	out, _, err := cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "Followed jq-update, renamed by hand to jq-1.8.\n")
	require.NotContains(t, out, "Git branch is gone")
	out, _, err = cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.NotContains(t, out, "Followed", "said once")
	require.Contains(t, out, "jq-1.8")
}

// A branch has one check at a time: ship doesn't queue a second beside one
// already queued for the same commit, and follows it instead, as a rerun
// resumes (decision 4). And it submits exactly the commit it checked: a
// branch that moved while its check ran is not submitted.
func TestShipKeepsToOneCheckAndItsCommit(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	cmdtest.WithScript(t, w, "passed")
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)

	_, _, err = cmdtest.Dockhand(t, "check", "--head", "--enqueue")
	require.NoError(t, err)
	g.Others = nil
	out, _, err := cmdtest.Dockhand(t, "ship", "--yes", "-v")
	require.NoError(t, err)
	require.Contains(t, out, "check-1 is already queued for these files; following it.\n")
	require.NotContains(t, out, "check-2", "no second check")
	require.Contains(t, out, "Opened #34901")

	// A new commit, which the next check builds.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n# reviewed\n"), 0o644))
	testsupport.Git(t, dir, "commit", "-q", "-am", "jq: note the review")

	// The build commits to the branch as it runs.
	script := filepath.Join(w.Home, "bin", "build-ports")
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
git -C "$MOVE_IN" commit -q --allow-empty -m "moved while checked"
cat > "$(dirname "$1")/result.json" <<JSON
{"version": 1, "targets": [{"id": "jq", "outcome": "passed"}]}
JSON
`), 0o755))
	cmdtest.Setenv(t, "MOVE_IN", dir)
	_, _, err = cmdtest.Dockhand(t, "ship", "--yes")
	require.Error(t, err)
	// What moves it on, branchstate's, is said last (R3-31).
	require.Regexp(t, `^jq-update moved to [0-9a-f]{7} while it was checked; nothing was submitted, since ship binds [0-9a-f]{7}\nNext: dockhand submit --branch jq-update$`, err.Error())
	require.Equal(t, "jq: update to 1.8.1", g.PRs[34901].Title)
	require.NotEqual(t, testsupport.Git(t, dir, "rev-parse", "HEAD"), testsupport.Git(t, g.Fork, "rev-parse", "dockhand/jq-update"), "the moved branch wasn't pushed")
}

// The description is edited in the process's run root, so a process that
// dies with the editor open leaves the buffer to the next one's sweep.
func TestTheDescriptionIsEditedInTheRunRoot(t *testing.T) {
	t.Parallel()
	record := filepath.Join(t.TempDir(), "path")
	editor := `edit() { printf '%s' "$1" > '` + record + `'; printf 'edited' > "$1"; }; edit`
	text, err := settings.EditText(t.Context(), editor, "draft")
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
// (the beekeeper-studio run's finding 4); where the profile chose not to
// build, as minmax does, it says nothing (field testing at 9c5919dd).
func TestSubmitsCheckLineNamesOnlyWhereItBuilt(t *testing.T) {
	t.Parallel()
	monterey := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	target := model.PlanTarget{ID: "beekeeper-studio", Target: model.Target{Name: "beekeeper-studio"}}
	evidence := &engine.Evidence{
		Run: model.Run{ID: "run_21", Number: 21},
		Plan: model.Plan{Environments: []model.Environment{monterey, tahoe}, Builds: []model.EnvironmentPlan{
			{Environment: monterey, Exclusions: []model.Exclusion{{Target: target.Target, Reason: "known_fail"}}},
			{Environment: tahoe, Order: []model.TargetID{target.ID}},
		}},
		Targets:    []engine.TargetEvidence{{Target: target, Passed: true, Outcomes: cmdtest.Cells([]model.TargetResult{{Outcome: model.OutcomeNotRun}, {Execution: "tart_b", Outcome: model.OutcomePassed}})}},
		Executions: map[model.ExecutionID]model.GuestExecution{"tart_b": {ID: "tart_b", Run: "run_21"}},
	}
	require.Equal(t, "passed on "+engine.DescribeEnvironment(tahoe)+" for this commit's files (check-21); nothing built on "+engine.DescribeEnvironment(monterey)+", where every port's Portfile excludes it",
		view.CheckWords(engine.SubmitPlan{Evidence: evidence}))
	sequoia := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	evidence.Plan.Environments = append(evidence.Plan.Environments, sequoia)
	evidence.Plan.Builds = append(evidence.Plan.Builds, model.EnvironmentPlan{Environment: sequoia, Exclusions: []model.Exclusion{{Target: target.Target, Reason: "minmax builds each port on its oldest and newest releases", Span: true}}})
	evidence.Targets[0].Outcomes = append(evidence.Targets[0].Outcomes, evidence.Targets[0].Outcomes[0])
	require.NotContains(t, view.CheckWords(engine.SubmitPlan{Evidence: evidence}), engine.DescribeEnvironment(sequoia), "minmax's choice isn't the ports' exclusion")

	// An accepted failure says so, and why (field testing, #35662).
	failed := &engine.Evidence{Run: model.Run{ID: "run_21", Number: 21}, Plan: model.Plan{Environments: []model.Environment{tahoe}},
		Targets: []engine.TargetEvidence{{Target: target, Outcomes: cmdtest.Cells([]model.TargetResult{{Execution: "tart_b", Outcome: model.OutcomeFailed, Phase: model.PhaseInstall}})}}}
	failed.Targets[0].Outcomes[0].Environment = tahoe
	require.Equal(t, "check-21: beekeeper-studio did not pass on "+engine.DescribeEnvironment(tahoe)+", accepted: fails at the base too (check-22)",
		view.CheckWords(engine.SubmitPlan{Evidence: failed, Accepted: map[string]engine.AcceptedFailure{"beekeeper-studio": {Kind: engine.AcceptedAtBase, Baselines: []string{"check-22"}}}}))
}

// restricted is GitHub's refusal of an app an organization hasn't
// approved, as the forge's client reports it.
type restricted struct{ error }

func (restricted) Is(target error) bool { return target == forge.ErrAppRestricted }

// A draft's preview names the command that marks it ready, and --ready's
// preview says what it would do in each case.
func TestTheReadyStepIsPreviewed(t *testing.T) {
	t.Parallel()
	draft := engine.SubmitPlan{Branch: model.Branch{PullRequest: &model.PullRequest{Number: 34901, Draft: true}},
		Existing: &forge.PullRequestObservation{PullRequest: forge.PullRequest{Ref: forge.PullRequestRef{Number: 34901}}}}
	require.True(t, strings.HasPrefix(view.PullRequestWords(draft), "updates #34901, a draft: dockhand submit --ready marks it ready for review"), view.PullRequestWords(draft))
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
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	_, _, err = cmdtest.Dockhand(t, "start", "other-work")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "archive", "other-work", "--keep-worktree")
	require.NoError(t, err)

	// Cleaned by hand, before status knows of the merge.
	testsupport.Git(t, w.Clone, "worktree", "remove", "--force", dir)
	testsupport.Git(t, w.Clone, "branch", "-D", "dockhand/jq-update")
	out, _, err := cmdtest.Dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "its Git branch is gone, and its pull request, #34901, may have merged\n")
	require.Contains(t, out, "dockhand status --refresh reads it")

	g.PRs[34901].State = forge.PullRequestMerged
	_, _, err = cmdtest.Dockhand(t, "status", "--refresh")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "clean", "other-work-missing")
	require.ErrorContains(t, err, "no tracked branch named other-work-missing")
	_, _, err = cmdtest.Dockhand(t, "start", "open-work")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "clean", "open-work")
	require.ErrorContains(t, err, "open-work is open, so there's nothing to clean")

	out, _, err = cmdtest.Dockhand(t, "clean", "jq-update")
	require.Equal(t, term.ExitFailure, term.ExitCode(err))
	require.Contains(t, out, "jq-update (#34901, merged at ")
	require.NotContains(t, out, "other-work", "an archived branch not named is left")
	require.Contains(t, out, "  remove   ada/macports-ports:dockhand/jq-update\n")
	out, _, err = cmdtest.Dockhand(t, "clean", "jq-update", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update: removed ada/macports-ports:dockhand/jq-update\n")
	require.DirExists(t, filepath.Join(w.Home, "Source", "macports-branches", "other-work"), "the archived branch's worktree stays")
}

// The Push line says what the blocker holds, not the push it stops (the
// rc6 full stage, D-S1).
func TestThePushLineSaysItWontPushOverAnotherPush(t *testing.T) {
	t.Parallel()
	plan := engine.SubmitPlan{Commit: strings.Repeat("7", 40), Replaces: true, OthersPushed: model.ObjectID(strings.Repeat("d", 40))}
	plan.RemoteHead.Exists, plan.RemoteHead.Object = true, strings.Repeat("d", 40)
	require.Equal(t, "won't push: someone else pushed ddddddd", view.PushWords(plan))
}

// The Checks line says where a port failed apart from where nothing of
// these files built it: check-323 passed grafana on macOS 12, and the line
// said it "did not pass on" 12 and 26 alike (field testing, grafana
// 13.2.4).
func TestCheckWordsSayWhatWasntBuilt(t *testing.T) {
	t.Parallel()
	monterey := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "21", Architecture: "arm64"}}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	grafana := model.PlanTarget{Target: model.Target{Name: "grafana"}}
	evidence := engine.Evidence{Run: model.Run{Number: 323}, Plan: model.Plan{Environments: []model.Environment{monterey, tahoe}}, Targets: []engine.TargetEvidence{{
		Target:    grafana,
		Unchecked: true,
		Outcomes: []engine.Cell{
			{TargetResult: model.TargetResult{Outcome: model.OutcomePassed}, Kind: engine.CellRecorded, Environment: monterey},
			{TargetResult: model.TargetResult{Outcome: model.OutcomeNotRun}, Kind: engine.CellNotRun, Environment: tahoe},
		},
	}}}
	said := view.CheckWords(engine.SubmitPlan{Evidence: &evidence})
	require.Equal(t, "check-323: grafana isn't built for these files on "+engine.DescribeEnvironment(tahoe), said)
}

// A submission that pushes to a fork its login doesn't own, as a sandbox
// or --remote lets it, says so beneath the account it pushes as (the rc9
// full stage's D-C4); someone else's pull request, pushed to as theirs,
// doesn't.
func TestASubmissionToAnotherAccountsForkIsSaid(t *testing.T) {
	t.Parallel()
	plan := engine.SubmitPlan{HeadRepository: "herbyg-test/macports-ports", Repository: "herbyg-test/macports-ports",
		Login: forge.Login{Account: "herbygillot", Source: "Dockhand macOS Keychain", From: "dockhand's login in the Keychain"}}
	var out bytes.Buffer
	view.WriteSubmitPlan(&out, plan, true)
	require.Contains(t, out.String(), "  As       herbygillot, from dockhand's login in the Keychain\n"+
		"           ! herbyg-test/macports-ports belongs to herbyg-test; you will push and create or update the pull request as herbygillot\n")
	plan.Theirs = true
	out.Reset()
	view.WriteSubmitPlan(&out, plan, true)
	require.NotContains(t, out.String(), "is herbyg-test's")
	plan.Theirs, plan.HeadRepository = false, "HerbyGillot/macports-ports"
	_, another := plan.ForkOwner()
	require.False(t, another, "GitHub's logins are the same in any case")
}
