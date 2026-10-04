package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestReviewShowsThenPostsOnlyWhenAsked(t *testing.T) {
	w := newWorld(t)
	g := withGitHub(t, w)
	testsupport.Git(t, w.upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	testsupport.Git(t, w.upstream, "commit", "-q", "-am", "Update jq docs")
	testsupport.Git(t, w.upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905, URL: "https://github.com/macports/macports-ports/pull/34905"}, Title: "Update jq docs", State: forge.PullRequestOpen}}

	_, _, err := dockhand(t, "review", "x")
	require.ErrorContains(t, err, `"x" is not a pull request number`)
	out, said, err := dockhand(t, "review", "34905")
	require.Contains(t, said, "Experimental: dockhand evaluates #34905's Portfiles, its author's,", "the release bar's notice")
	require.NoError(t, err)
	require.Contains(t, out, "review of #34905 \"Update jq docs\", as @ada (read access to macports/macports-ports)\n  1 commit changing jq\n")
	require.Contains(t, out, `should start with the port it changes: "jq: …" [subject-port]`)
	require.Contains(t, out, "Nothing was posted; --comment or --request-changes posts it.\n")
	require.Empty(t, g.Reviews)

	out, _, err = dockhand(t, "review", "34905", "--markdown")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, "Reviewed at "))

	var stdout, errs bytes.Buffer
	err = Run(t.Context(), []string{"review", "#34905"}, Streams{In: strings.NewReader("r\nc\n"), Out: &stdout, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Contains(t, errs.String(), "? post as: c comment · e edit first · n don't post", "read access can't request changes")
	require.Contains(t, errs.String(), "Requesting changes is left to people with write or triage access; you have read access.")
	require.Contains(t, stdout.String(), "✓ review posted on #34905: a comment, 0 inline comments\n  https://github.com/macports/macports-ports/pull/34905#pullrequestreview-1\n")
	require.Len(t, g.Reviews, 1)
	require.False(t, g.Reviews[0].RequestChanges)

	_, _, err = dockhand(t, "review", "34905", "--request-changes")
	require.ErrorContains(t, err, "requesting changes is left to people with write or triage access")
}

func TestAdoptSomeonesPullRequest(t *testing.T) {
	w := newWorld(t)
	g := withGitHub(t, w)
	testsupport.Git(t, w.upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	testsupport.Git(t, w.upstream, "commit", "-q", "-am", "jq: document the options")
	testsupport.Git(t, w.upstream, "update-ref", "refs/pull/34905/head", "contrib")
	testsupport.Git(t, w.upstream, "switch", "-q", "master")
	g.Theirs = map[int]forge.PullRequest{34905: {Ref: forge.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905},
		HeadRepository: "newcontrib/macports-ports", HeadBranch: "patch-1", Title: "jq: document the options", State: forge.PullRequestOpen, Author: "newcontrib", MaintainerCanModify: true}}

	_, _, err := dockhand(t, "adopt", "x", "--pr", "34905")
	require.ErrorContains(t, err, "adopt takes a branch or --pr, not both")
	out, said, err := dockhand(t, "adopt", "--pr", "34905")
	require.NoError(t, err)
	require.Contains(t, said, "Experimental: dockhand evaluates #34905's Portfiles, @newcontrib's, with MacPorts on this Mac", "the release bar's notice")
	require.Equal(t, "Adopted pr-34905: \"jq: document the options\" by @newcontrib, 1 commit, changing jq; maintainers can edit.\nDirectory: ~/Source/macports-branches/pr-34905\n", out)
	require.FileExists(t, filepath.Join(w.home, "Source", "macports-branches", "pr-34905", "textproc/jq/Portfile"))
	out, _, err = dockhand(t, "adopt", "--pr", "34905")
	require.NoError(t, err)
	require.Equal(t, "#34905 is already tracked, as pr-34905.\n", out)
}

// A review's terminal output says what the assessment found, marked as
// update marks it, and the dependents, or why either wasn't read.
func TestAReviewSaysTheAssessmentAndTheDependents(t *testing.T) {
	report := engine.ReviewReport{Ref: forge.PullRequestRef{Number: 34620}, Title: "libuv: update to 1.52.1", Login: "you", Permission: "none",
		Head: strings.Repeat("b", 40), Base: strings.Repeat("a", 40), Ports: []string{"libuv"},
		Upstream: []engine.PortComparison{{Port: "libuv", Comparison: model.UpstreamComparison{Changes: []model.UpstreamChange{
			{Kind: "patch", Message: "patch-libuv-legacy.diff, which the base applied, is dropped, and no longer applies to 1.52.1's source: 5 out of 5 hunks FAILED"},
		}}}},
		Dependents: []engine.Dependent{{Name: "ttyd", Phases: []string{"library"}}, {Name: "luv", Phases: []string{"library"}}}}
	var out bytes.Buffer
	writeReview(&out, report)
	require.Contains(t, out.String(), "  Upstream, against MacPorts' ports at the pull request's bbbbbbb, off master at aaaaaaa:\n    · patch-libuv-legacy.diff, which the base applied, is dropped, and no longer applies to 1.52.1's source: 5 out of 5 hunks FAILED\n")
	require.Contains(t, out.String(), "  2 dependents, from the index at aaaaaaa: ttyd (library), luv (library); candidates to look at.\n")

	report.Upstream, report.UpstreamUnread, report.Dependents, report.DependentsUnread = nil, "assessing a revision needs MacPorts' evaluator", nil, "reading the port index: no index"
	out.Reset()
	writeReview(&out, report)
	require.Contains(t, out.String(), "  · upstream's change wasn't assessed: assessing a revision needs MacPorts' evaluator\n")
	require.Contains(t, out.String(), "  Dependents weren't read: reading the port index: no index.\n")
}
