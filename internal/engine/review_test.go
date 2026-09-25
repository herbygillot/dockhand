package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/record"
)

// contribution puts someone's pull request #34905 on the upstream: an
// update to jq that keeps its revision, then a follow-up commit.
func contribution(t *testing.T, f fixture, fake *fakeForge) {
	t.Helper()
	run(t, f.upstream, "switch", "-q", "-c", "contrib")
	write(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\nrevision 1\n"})
	commitAs(t, f.upstream, "New newcontrib@example.org", "Update jq")
	write(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\nrevision 1\n# docs\n"})
	commitAs(t, f.upstream, "New newcontrib@example.org", "jq: fix typo")
	run(t, f.upstream, "update-ref", "refs/pull/34905/head", "contrib")
	run(t, f.upstream, "switch", "-q", "master")
	fake.prs[34905] = &record.PullRequest{Ref: record.PullRequestRef{Forge: forge.GitHub, Repository: UpstreamRepository, Number: 34905, URL: "https://github.com/macports/macports-ports/pull/34905"},
		HeadRepository: "newcontrib/macports-ports", HeadBranch: "patch-1", State: record.PullRequestOpen, Title: "Update jq"}
}

func TestReviewAppliesTheRulesAndRemembersWhatItFound(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	fake := f.withFork(t, e)
	contribution(t, f, fake)

	report, err := e.Review(t.Context(), 34905)
	require.NoError(t, err)
	require.Equal(t, "Update jq", report.Title)
	require.Len(t, report.Commits, 2)
	require.Equal(t, []string{"jq"}, report.Ports)
	require.Equal(t, []string{"subject-port", "follow-up", "revision-after-update"}, findingCodes(report.Findings))
	require.Contains(t, report.Summary(), "2 commits changing jq; MacPorts asks for one commit per logical change.")
	require.Equal(t, []forge.ReviewComment{{Path: "textproc/jq/Portfile", Line: 3, Body: "revision is 1 after a version update; MacPorts expects 0 [revision-after-update]"}}, report.Comments())
	require.False(t, report.CanRequestChanges(), "no access to the repository")
	require.Contains(t, report.Markdown(), "- ✗ commit ")
	require.Contains(t, report.Markdown(), "Not checked here: `port lint` and the build")

	_, err = e.PostReview(t.Context(), report, report.Markdown(), true)
	require.ErrorContains(t, err, "requesting changes is left to people with write or triage access")
	fake.permission = "triage"
	report, err = e.Review(t.Context(), 34905)
	require.NoError(t, err)
	url, err := e.PostReview(t.Context(), report, report.Markdown(), true)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/macports/macports-ports/pull/34905#pullrequestreview-1", url)
	require.True(t, fake.reviews[0].RequestChanges)
	require.Equal(t, report.Head, fake.reviews[0].Commit)

	// The contributor squashes and fixes the revision.
	run(t, f.upstream, "switch", "-q", "-c", "contrib-2", "master")
	write(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\nrevision 0\n# docs\n"})
	commitAs(t, f.upstream, "New newcontrib@example.org", "jq: update to 1.8.1")
	run(t, f.upstream, "update-ref", "refs/pull/34905/head", "contrib-2")
	run(t, f.upstream, "switch", "-q", "master")
	again, err := e.Review(t.Context(), 34905)
	require.NoError(t, err)
	require.Empty(t, again.Findings)
	require.Equal(t, report.Head, string(again.Previous.Head))
	require.Len(t, again.Resolved, 3)
	require.Contains(t, again.Markdown(), "Resolved since the review at ")
	require.Contains(t, again.Markdown(), "[follow-up]")
}

func findingCodes(findings []commitrules.Finding) []string {
	var codes []string
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	return codes
}
