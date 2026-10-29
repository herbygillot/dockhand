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
	out, said, err := cmdtest.Dockhand(t, "review", "34905")
	require.Contains(t, said, "Evaluating #34905's author's Portfiles on this Mac, for #34905", "a command with no terminal is a person's, and says it (the trust rule)")
	require.EqualError(t, err, "No review was posted. In a script, use --comment or --request-changes to post it.")
	require.Contains(t, out, "review of #34905 \"Update jq docs\", as @ada (read access to macports/macports-ports)\n  1 commit changing jq\n")
	require.Contains(t, cmdtest.Flat(out), cmdtest.Flat(`should start with the port it changes: "jq: …" [subject-port]`))
	require.Empty(t, g.Reviews)

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

// review --check adopts the pull request and checks it, saying whose
// Portfiles its plan evaluates on this Mac (the trust rule, Then's item
// 12); again, it checks the branch it was adopted as.
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

	out, said, err := cmdtest.Dockhand(t, "review", "34905", "--check", "--on", "command")
	require.NoError(t, err, said)
	head := testsupport.Git(t, w.Upstream, "rev-parse", "--short=7", "contrib")
	require.Contains(t, out, "Adopted #34905 at "+head+" as pr-34905; checking it.")
	require.Contains(t, out, "Passed")
	require.Contains(t, said, "Evaluating @newcontrib's Portfiles on this Mac, for pr-34905")
	require.FileExists(t, filepath.Join(w.Home, "Source", "macports-branches", "pr-34905", "textproc/jq/Portfile"))
	out, _, _ = cmdtest.Dockhand(t, "review", "34905", "--check", "--on", "command")
	require.Contains(t, out, "#34905 is tracked as pr-34905, at the head reviewed, "+head+"; checking it.")
}

// review --check checks the commit it reviewed: after the author pushes B
// over an adopted A, the branch is moved to B, and the JSON's review and
// its check name one commit, where they named B and A (Codex's review of
// 2ad896d2, W2, its probe inverted).
func TestReviewCheckChecksTheCommitItReviewed(t *testing.T) {
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

	result, err := cmdtest.JSONOf(t, "review", "34905", "--check", "--on", "command")
	require.NoError(t, err)
	require.Equal(t, b, cmdtest.Dig(t, result.Result, "head"))
	require.Equal(t, b, cmdtest.Dig(t, result.Result, "check", "revision", "commit"), "the check is of the commit reviewed")
	require.Equal(t, "passed", cmdtest.Dig(t, result.Result, "check", "run", "state"))
}
