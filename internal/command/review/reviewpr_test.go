package review

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestReviewShowsThenPostsOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	g := cmdtest.WithGitHub(t, w)
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "Update jq docs")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905, URL: "https://github.com/macports/macports-ports/pull/34905"}, Title: "Update jq docs", State: forge.PullRequestOpen}}

	_, _, err := cmdtest.Dockhand(t, "review", "x")
	require.ErrorContains(t, err, `"x" is not a pull request number`)
	// A script's review of another person's pull request is text-only
	// unless --evaluate says otherwise, and says what it left out (#162).
	out, said, err := cmdtest.Dockhand(t, "review", "34905")
	require.NotContains(t, said, "Evaluating", "nothing of the author's is evaluated")
	require.EqualError(t, err, "No review was posted. In a script, use --comment or --request-changes to post it.")
	require.True(t, strings.HasPrefix(out, "Text-only review: "), out)
	require.Contains(t, cmdtest.Flat(out), "Text-only review: #34905's Portfiles were not evaluated. Not run: restated values, path-style dependencies, the upstream comparison.")
	require.Contains(t, out, "review of #34905 \"Update jq docs\", as @ada (read access to macports/macports-ports)\n  1 commit changing jq\n")
	require.NotContains(t, out, "[subject-port]", "a port only the head might define isn't read, so the subject isn't judged by it")
	require.Empty(t, g.Reviews)
	out, _, err = cmdtest.Dockhand(t, "review", "34905", "--markdown")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, "Text-only review: "), "the posted text says it too")

	out, said, err = cmdtest.Dockhand(t, "review", "34905", "--evaluate")
	require.Contains(t, said, "Evaluating #34905's author's Portfiles on this Mac, for #34905", "--evaluate allows it, and the door says whose (the trust rule)")
	require.EqualError(t, err, "No review was posted. In a script, use --comment or --request-changes to post it.")
	require.NotContains(t, out, "Text-only review")
	require.Contains(t, cmdtest.Flat(out), cmdtest.Flat(`should start with the port it changes: "jq: …" [subject-port]`))

	out, _, err = cmdtest.Dockhand(t, "review", "34905", "--markdown")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, "Reviewed at "))

	var stdout, errs bytes.Buffer
	err = cmdtest.RunFor(t, t.Context(), []string{"review", "#34905"}, term.Streams{In: strings.NewReader("r\nc\n"), Out: &stdout, Err: &errs}.AsTerminal())
	require.NoError(t, err)
	require.Contains(t, errs.String(), "? post as: c comment · e edit first · n don't post", "read access can't request changes")
	require.Contains(t, errs.String(), "Requesting changes requires write or triage access; your access is read access.")
	require.Contains(t, stdout.String(), "✓ review posted on #34905: a comment, 0 inline comments\n  https://github.com/macports/macports-ports/pull/34905#pullrequestreview-1\n")
	require.Len(t, g.Reviews, 1)
	require.False(t, g.Reviews[0].RequestChanges)

	_, _, err = cmdtest.Dockhand(t, "review", "34905", "--request-changes")
	require.ErrorContains(t, err, "requesting changes is left to people with write or triage access")
}

func TestAdoptSomeonesPullRequest(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	g := cmdtest.WithGitHub(t, w)
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document the options")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905},
		HeadRepository: "newcontrib/macports-ports", HeadBranch: "patch-1", Title: "jq: document the options", State: forge.PullRequestOpen, Author: "newcontrib", MaintainerCanModify: true}}

	_, _, err := cmdtest.Dockhand(t, "adopt", "x", "--pr", "34905")
	require.ErrorContains(t, err, "adopt takes a branch or --pr, not both")
	out, said, err := cmdtest.Dockhand(t, "adopt", "--pr", "34905")
	require.NoError(t, err)
	require.NotContains(t, said, "Experimental", "adopting evaluates nothing; what does says whose Portfiles (the trust rule)")
	require.Equal(t, cmdtest.Flat("Adopted pr-34905: \"jq: document the options\" by @newcontrib, 1 commit, changing jq; maintainers can edit.\nDirectory: ~/Source/macports-branches/pr-34905\n"), cmdtest.Flat(out))
	require.FileExists(t, filepath.Join(w.Home, "Source", "macports-branches", "pr-34905", "textproc/jq/Portfile"))
	out, _, err = cmdtest.Dockhand(t, "adopt", "--pr", "34905")
	require.NoError(t, err)
	require.Equal(t, "#34905 is already tracked, as pr-34905.\n", out)
}

// A review's terminal output says what the assessment found, marked as
// update marks it, and the dependents, or why either wasn't read.
func TestAReviewSaysTheAssessmentAndTheDependents(t *testing.T) {
	t.Parallel()
	report := engine.ReviewReport{Ref: forge.PullRequestRef{Number: 34620}, Title: "libuv: update to 1.52.1", Login: "you", Permission: "none",
		Head: strings.Repeat("b", 40), Base: strings.Repeat("a", 40), Ports: []string{"libuv"},
		Upstream: []engine.PortComparison{{Port: "libuv", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{
			{Kind: "patch", Message: "patch-libuv-legacy.diff, which the base applied, is dropped, and no longer applies to 1.52.1's source: 5 out of 5 hunks FAILED"},
		}}}},
		Dependents: []treequery.Dependent{{Name: "ttyd", Phases: []string{"library"}}, {Name: "luv", Phases: []string{"library"}}}}
	var out bytes.Buffer
	writeReview(&out, report)
	require.Contains(t, out.String(), "  Upstream comparison using ports at pull request commit bbbbbbb (based on master aaaaaaa):\n      patch-libuv-legacy.diff, which the base applied, is dropped, and no longer applies to 1.52.1's source: 5 out of 5 hunks FAILED\n")
	require.Contains(t, out.String(), "  2 dependents from the index at aaaaaaa: ttyd (library), luv (library). Review these candidates.\n")

	report.Upstream, report.UpstreamUnread, report.Dependents, report.DependentsUnread = nil, "assessing a revision needs MacPorts' evaluator", nil, "reading the port index: no index"
	out.Reset()
	writeReview(&out, report)
	require.Contains(t, out.String(), "  ! Upstream changes could not be assessed: assessing a revision needs MacPorts' evaluator\n")
	require.Contains(t, out.String(), "  Could not determine dependents: reading the port index: no index.\n")
}

// review --check adopts the pull request and checks it, once the person
// allows its Portfiles to be evaluated, since planning evaluates them on
// this Mac: a script without --evaluate is refused before anything is
// adopted (#162). Again, it checks the branch it was adopted as.
func TestReviewCheckAdoptsAndChecks(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document the options")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905},
		HeadRepository: "newcontrib/macports-ports", HeadBranch: "patch-1", Title: "jq: document the options", State: forge.PullRequestOpen, Author: "newcontrib", MaintainerCanModify: true}}

	_, _, err := cmdtest.Dockhand(t, "review", "34905", "--check", "--on", "command")
	require.ErrorContains(t, err, "review --check plans the check by evaluating #34905's Portfiles on this Mac, which needs your yes; nothing was checked. In a script, add --evaluate")
	require.NoDirExists(t, filepath.Join(w.Home, "Source", "macports-branches", "pr-34905"), "nothing is adopted")
	out, said, err := cmdtest.Dockhand(t, "review", "34905", "--check", "--on", "command", "--evaluate")
	require.NoError(t, err, said)
	head := testsupport.Git(t, w.Upstream, "rev-parse", "--short=7", "contrib")
	require.Contains(t, out, "Adopted #34905 at "+head+" as pr-34905; checking it.")
	require.Contains(t, out, "Passed")
	require.FileExists(t, filepath.Join(w.Home, "Source", "macports-branches", "pr-34905", "textproc/jq/Portfile"))
	out, _, _ = cmdtest.Dockhand(t, "review", "34905", "--check", "--on", "command")
	require.Contains(t, out, "#34905 is tracked as pr-34905, at the head reviewed, "+head+"; checking it.", "the head allowed is kept")
}

// review --check checks the commit it reviewed: after the author pushes B
// over an adopted A, the branch is moved to B, and the JSON's review and
// its check name one commit, where they named B and A (Codex's review of
// 2ad896d2, W2, its probe inverted).
func TestReviewCheckChecksTheCommitItReviewed(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	path := filepath.Join(w.Upstream, "textproc/jq/Portfile")
	require.NoError(t, os.WriteFile(path, []byte("name jq\n# version A\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document version A")
	a := testsupport.Git(t, w.Upstream, "rev-parse", "HEAD")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "HEAD")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	pr := forge.PullRequest{Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905}, HeadRepository: "contributor/macports-ports", HeadBranch: "contrib", RemoteHead: model.ObjectID(a), Title: "jq: docs", State: forge.PullRequestOpen, Author: "contributor", MaintainerCanModify: true}
	g.Theirs = map[int]forge.PullRequest{34905: pr}
	_, said, err := cmdtest.Dockhand(t, "adopt", "--pr", "34905")
	require.NoError(t, err, said)

	testsupport.Git(t, w.Upstream, "switch", "-q", "contrib")
	require.NoError(t, os.WriteFile(path, []byte("name jq\n# version B\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document version B")
	b := testsupport.Git(t, w.Upstream, "rev-parse", "HEAD")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "HEAD")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	pr.RemoteHead = model.ObjectID(b)
	g.Theirs[34905] = pr

	result, err := cmdtest.JSONOf(t, "review", "34905", "--check", "--on", "command", "--evaluate")
	require.NoError(t, err)
	require.Equal(t, b, cmdtest.Dig(t, result.Result, "head"))
	require.Equal(t, b, cmdtest.Dig(t, result.Result, "check", "revision", "commit"), "the check is of the commit reviewed")
	require.Equal(t, "passed", cmdtest.Dig(t, result.Result, "check", "run", "state"))
}

// review --check refuses, before it reviews or fetches anything, a pull
// request whose number an open branch tracks in another repository than
// the one DOCKHAND_PULL_REQUESTS names: pr-34905, adopted from MacPorts,
// under a sandbox (the rc6 full stage's G22).
func TestReviewCheckRefusesABranchOfAnotherRepository(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document the options")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905},
		HeadRepository: "newcontrib/macports-ports", HeadBranch: "patch-1", Title: "jq: document the options", State: forge.PullRequestOpen, Author: "newcontrib", MaintainerCanModify: true}}
	_, said, err := cmdtest.Dockhand(t, "adopt", "--pr", "34905")
	require.NoError(t, err, said)
	refs := testsupport.Git(t, w.Clone, "for-each-ref", "refs/dockhand/reviews")

	cmdtest.Setenv(t, "DOCKHAND_PULL_REQUESTS", "herbyg-test/macports-ports")
	out, _, err := cmdtest.Dockhand(t, "review", "34905", "--check", "--on", "command")
	require.ErrorContains(t, err, "pr-34905 is macports/macports-ports#34905, and DOCKHAND_PULL_REQUESTS names herbyg-test/macports-ports; unset it to work on pr-34905")
	require.NotContains(t, out, "review of #34905", "nothing was reviewed")
	require.Equal(t, refs, testsupport.Git(t, w.Clone, "for-each-ref", "refs/dockhand/reviews"), "nothing was fetched")
	_, _, err = cmdtest.Dockhand(t, "adopt", "--pr", "34905")
	require.ErrorContains(t, err, "unset it to work on pr-34905")
	cmdtest.Setenv(t, "DOCKHAND_PULL_REQUESTS", "")
}

// review --check --comment posts the review saying the check, which
// failed, and ends with the check's failure: the post's success was the
// command's, and its text said Dockhand hadn't built it (Codex's workflow
// review of 1caa6c3d, finding 7, its probe).
func TestAPostedReviewSaysItsFailedCheckAndFails(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.WithScript(t, w, "failed")
	g := cmdtest.WithGitHub(t, w)
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document the options")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905}, HeadRepository: "newcontrib/macports-ports", HeadBranch: "patch-1", Title: "jq: document the options", State: forge.PullRequestOpen, Author: "newcontrib", MaintainerCanModify: true}}
	out, said, err := cmdtest.Dockhand(t, "review", "34905", "--check", "--on", "command", "--comment", "--evaluate")
	require.Len(t, g.Reviews, 1, "posted all the same", out, said)
	require.Equal(t, 2, term.ExitCode(err), "the check's failure")
	require.Contains(t, g.Reviews[0].Body, "Dockhand ran `port lint` and the build in check-1 on command, which failed")
	require.NotContains(t, g.Reviews[0].Body, "Not checked by Dockhand")
}

// On a terminal, review asks before it evaluates another person's
// Portfiles, listing what it changes, with the diff one key away; No, the
// default, is a text-only review and isn't kept. Yes is kept for the
// head, and the author's new push asks again, about what changed since
// (#162).
func TestReviewAsksBeforeItEvaluatesSomeoneElsesPortfiles(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	g := cmdtest.WithGitHub(t, w)
	path := filepath.Join(w.Upstream, "textproc/jq/Portfile")
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(path, []byte("name jq\n# docs\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document the options")
	first := testsupport.Git(t, w.Upstream, "rev-parse", "--short=7", "HEAD")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905}, Title: "jq: document the options", State: forge.PullRequestOpen, Author: "newcontrib"}}
	review := func(answers string) (string, string) {
		var stdout, errs bytes.Buffer
		err := cmdtest.RunFor(t, t.Context(), []string{"review", "34905"}, term.Streams{In: strings.NewReader(answers), Out: &stdout, Err: &errs}.AsTerminal())
		require.NoError(t, err, errs.String())
		return stdout.String(), errs.String()
	}

	out, said := review("\nn\n")
	require.Contains(t, cmdtest.Flat(out), cmdtest.Flat("#34905 is @newcontrib's. A full review evaluates its Portfiles with MacPorts on this Mac, as you."))
	require.Contains(t, out, "textproc/jq/Portfile  +1 -0")
	require.Contains(t, said, "? Evaluate them: y yes · d show the diff first · n review the text only  [n]")
	require.Contains(t, out, "Text-only review: ")
	require.NotContains(t, said, "Evaluating @newcontrib's")

	out, said = review("d\ny\nn\n")
	require.Contains(t, out, "+# docs", "d shows the diff")
	require.Contains(t, said, "Evaluating @newcontrib's Portfiles on this Mac, for #34905")
	require.NotContains(t, out, "Text-only review")

	_, said = review("n\n")
	require.NotContains(t, said, "? Evaluate them", "the head allowed asks nothing")

	testsupport.Git(t, w.Upstream, "switch", "-q", "contrib")
	require.NoError(t, os.WriteFile(path, []byte("name jq\n# docs\n# more\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document more")
	second := testsupport.Git(t, w.Upstream, "rev-parse", "--short=7", "HEAD")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	out, said = review("\nn\n")
	require.Contains(t, out, "#34905 has a new head, "+second+". You allowed "+first+" on ")
	require.Contains(t, out, "textproc/jq/Portfile  +1 -0   (since "+first+")")
	require.Contains(t, said, "? Evaluate them: y yes · d show what changed · n review the text only  [n]")
}

// A review of the person's own pull request asks nothing.
func TestReviewOfYourOwnPullRequestAsksNothing(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	g := cmdtest.WithGitHub(t, w)
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document the options")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905}, Title: "jq: document the options", State: forge.PullRequestOpen, Author: "ada"}}
	result, err := cmdtest.JSONOf(t, "review", "34905")
	require.Error(t, err, "nothing posted, in a script")
	require.Equal(t, true, cmdtest.Dig(t, result.Result, "evaluated"))
	require.Empty(t, cmdtest.Dig(t, result.Result, "left_out"))
}

// On an adopted pull request's branch, a command that evaluates its
// Portfiles in a script is refused, naming the command that allows it;
// --yes doesn't answer; adopt --pr --evaluate does, for the head adopted.
// On a terminal the command asks (#162).
func TestAnAdoptedBranchsPortfilesNeedTheAnswer(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	testsupport.Git(t, w.Upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: document the options")
	testsupport.Git(t, w.Upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.Upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905},
		HeadRepository: "newcontrib/macports-ports", HeadBranch: "patch-1", Title: "jq: document the options", State: forge.PullRequestOpen, Author: "newcontrib", MaintainerCanModify: true}}
	_, said, err := cmdtest.Dockhand(t, "adopt", "--pr", "34905")
	require.NoError(t, err, said)

	_, _, err = cmdtest.Dockhand(t, "check", "-b", "pr-34905", "--on", "command", "-y")
	require.ErrorContains(t, err, "dockhand adopt --pr 34905 --evaluate allows it", "--yes doesn't answer it")

	var stdout, errs bytes.Buffer
	err = cmdtest.RunFor(t, t.Context(), []string{"check", "-b", "pr-34905", "--on", "command", "--plan"}, term.Streams{In: strings.NewReader("n\n"), Out: &stdout, Err: &errs}.AsTerminal())
	require.ErrorContains(t, err, "dockhand adopt --pr 34905 --evaluate allows it", "no is no")
	require.Contains(t, errs.String(), "? Evaluate them: y yes · d show the diff first · n no  [n]")

	_, said, err = cmdtest.Dockhand(t, "adopt", "--pr", "34905", "--evaluate")
	require.NoError(t, err, said)
	out, said, err := cmdtest.Dockhand(t, "check", "-b", "pr-34905", "--on", "command")
	require.NoError(t, err, said)
	require.Contains(t, out, "Passed")
}
