package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
)

func TestReviewShowsThenPostsOnlyWhenAsked(t *testing.T) {
	w := newWorld(t)
	g := withGitHub(t, w)
	gitRun(t, w.upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	gitRun(t, w.upstream, "commit", "-q", "-am", "Update jq docs")
	gitRun(t, w.upstream, "update-ref", "refs/pull/34905/head", "contrib")
	gitRun(t, w.upstream, "switch", "-q", "master")
	g.theirs = map[int]record.PullRequest{34905: {Ref: record.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905, URL: "https://github.com/macports/macports-ports/pull/34905"}, Title: "Update jq docs", State: record.PullRequestOpen}}

	_, _, err := dockhand(t, "review", "x")
	require.ErrorContains(t, err, `"x" is not a pull request number`)
	out, _, err := dockhand(t, "review", "34905")
	require.NoError(t, err)
	require.Contains(t, out, "review of #34905 \"Update jq docs\", as @ada (read access to macports/macports-ports)\n  1 commit changing jq\n")
	require.Contains(t, out, `should start with the port it changes: "jq: …" [subject-port]`)
	require.Contains(t, out, "Nothing was posted; --comment or --request-changes posts it.\n")
	require.Empty(t, g.reviews)

	out, _, err = dockhand(t, "review", "34905", "--markdown")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, "Reviewed at "))

	var stdout, errs bytes.Buffer
	err = Run(t.Context(), []string{"review", "#34905"}, Streams{In: strings.NewReader("r\nc\n"), Out: &stdout, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Contains(t, errs.String(), "? post as: c comment · e edit first · n don't post", "read access can't request changes")
	require.Contains(t, errs.String(), "Requesting changes is left to people with write or triage access; you have read access.")
	require.Contains(t, stdout.String(), "✓ review posted on #34905: a comment, 0 inline comments\n  https://github.com/macports/macports-ports/pull/34905#pullrequestreview-1\n")
	require.Len(t, g.reviews, 1)
	require.False(t, g.reviews[0].RequestChanges)

	_, _, err = dockhand(t, "review", "34905", "--request-changes")
	require.ErrorContains(t, err, "requesting changes is left to people with write or triage access")
}

func TestAdoptSomeonesPullRequest(t *testing.T) {
	w := newWorld(t)
	g := withGitHub(t, w)
	gitRun(t, w.upstream, "switch", "-q", "-c", "contrib")
	require.NoError(t, os.WriteFile(filepath.Join(w.upstream, "textproc/jq/Portfile"), []byte("name jq\n# docs\n"), 0o644))
	gitRun(t, w.upstream, "commit", "-q", "-am", "jq: document the options")
	gitRun(t, w.upstream, "update-ref", "refs/pull/34905/head", "contrib")
	gitRun(t, w.upstream, "switch", "-q", "master")
	g.theirs = map[int]record.PullRequest{34905: {Ref: record.PullRequestRef{Forge: "github", Repository: "macports/macports-ports", Number: 34905},
		HeadRepository: "newcontrib/macports-ports", HeadBranch: "patch-1", Title: "jq: document the options", State: record.PullRequestOpen, Author: "newcontrib", MaintainerCanModify: true}}

	_, _, err := dockhand(t, "adopt", "x", "--pr", "34905")
	require.ErrorContains(t, err, "adopt takes a branch or --pr, not both")
	out, _, err := dockhand(t, "adopt", "--pr", "34905")
	require.NoError(t, err)
	require.Equal(t, "Adopted pr-34905: \"jq: document the options\" by @newcontrib, 1 commit, changing jq; maintainers can edit.\nDirectory: ~/src/macports-branches/pr-34905\n", out)
	require.FileExists(t, filepath.Join(w.home, "src", "macports-branches", "pr-34905", "textproc/jq/Portfile"))
	out, _, err = dockhand(t, "adopt", "--pr", "34905")
	require.NoError(t, err)
	require.Equal(t, "#34905 is already tracked, as pr-34905.\n", out)
}
