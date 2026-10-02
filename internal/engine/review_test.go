package engine

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/commitrules"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
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
	fake.prs[34905] = &forge.PullRequest{Ref: forge.PullRequestRef{Forge: forge.GitHub, Repository: UpstreamRepository, Number: 34905, URL: "https://github.com/macports/macports-ports/pull/34905"},
		HeadRepository: "newcontrib/macports-ports", HeadBranch: "patch-1", State: forge.PullRequestOpen, Title: "Update jq"}
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

func TestAdoptSomeonesPullRequestAndPushOnlyWhereGitHubAllows(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	contribution(t, f, fake)
	theirs := filepath.Join(filepath.Dir(f.upstream), "newcontrib.git")
	run(t, filepath.Dir(f.upstream), "clone", "-q", "--bare", f.upstream, theirs)
	run(t, theirs, "branch", "-f", "patch-1", "contrib")
	run(t, f.clone, "remote", "add", "newcontrib", theirs)
	fake.repos = map[string]string{"newcontrib/macports-ports": theirs}
	pr := fake.prs[34905]
	pr.Author = "newcontrib"
	// A part dockhand would otherwise refresh, as it last saw it.
	pr.Body = "#### Description\n\ntheirs\n\n###### Tested on\n\nmacOS 15, by hand\n"

	adopted, err := e.AdoptPullRequest(t.Context(), 34905)
	require.NoError(t, err)
	require.Equal(t, "pr-34905", adopted.Branch.Name)
	require.Equal(t, 2, adopted.Commits)
	require.Equal(t, []string{"jq"}, adopted.Scope.PortNames())
	require.Equal(t, "newcontrib", adopted.Author)
	require.FileExists(t, filepath.Join(adopted.Branch.Worktree, "textproc/jq/Portfile"))
	require.Equal(t, run(t, theirs, "rev-parse", "patch-1"), string(adopted.Branch.PullRequest.Pushed))
	again, err := e.AdoptPullRequest(t.Context(), 34905)
	require.NoError(t, err)
	require.True(t, again.Already)

	write(t, adopted.Branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\nrevision 0\n# docs\n"})
	run(t, adopted.Branch.Worktree, "commit", "-q", "-am", "jq: reset revision")
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: adopted.Branch, NoCheck: true})
	require.NoError(t, err)
	require.Equal(t, "newcontrib/macports-ports:patch-1", plan.Head())
	require.Contains(t, plan.Blocking[0], "@newcontrib's #34905 doesn't let maintainers push to newcontrib/macports-ports:patch-1")

	pr.MaintainerCanModify = true
	fake.permission = "read"
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: adopted.Branch, NoCheck: true})
	require.NoError(t, err)
	require.Contains(t, plan.Blocking[0], "needs write access to macports/macports-ports, and you have read")

	fake.permission = "write"
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: adopted.Branch, NoCheck: true})
	require.NoError(t, err)
	require.Empty(t, plan.Blocking)
	require.True(t, plan.Theirs)
	require.Equal(t, DescriptionSections{Description: SectionKept, Types: SectionKept, TestedOn: SectionKept}, plan.Sections, "someone else's description is never rewritten")
	_, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: adopted.Branch, NoCheck: true, Note: new("Their tests need a network.")})
	require.ErrorContains(t, err, "--note: #34905 was opened from newcontrib/macports-ports, so its description stays theirs", "a note would never be given")
	_, err = e.ApplySubmit(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, run(t, adopted.Branch.Worktree, "rev-parse", "HEAD"), run(t, theirs, "rev-parse", "patch-1"), "pushed to their branch")
	require.Empty(t, fake.updated, "their title and description are theirs")
	require.Empty(t, fake.created)
}

// A review says what update would of someone's pull request: what
// upstream's change means for each port it changes, assessed as a branch's
// revision is, and the ports that depend on them (the libuv run's finding
// 2). Nothing is recorded; where the engine can't assess, it says so.
func TestReviewSaysWhatUpdateWouldOfSomeonesPullRequest(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	fake := f.withFork(t, e)
	contribution(t, f, fake)
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{
		"textproc/jq":  {{Name: "jq"}},
		"textproc/jaq": {{Name: "jaq", Dependencies: []macports.Dependency{{Port: "jq", Phase: "lib"}}}},
	}}

	report, err := e.Review(t.Context(), 34905)
	require.NoError(t, err)
	require.Equal(t, "assessing a revision needs MacPorts' evaluator", report.UpstreamUnread, "a reader that can't plan archives")
	require.Contains(t, report.Markdown(), "Upstream's change wasn't assessed: assessing a revision needs MacPorts' evaluator.")

	trees, err := e.Repo.CommitTrees(t.Context(), []string{report.Base, report.Head})
	require.NoError(t, err)
	p := newPlanner(t)
	e.ArchivePlanner = p
	p.add(model.ObjectID(trees[report.Base]), plannedPort{info: macports.PortInfo{Name: "jq", Version: "1.7.1"}, archives: map[string]map[string]string{"jq-1.7.1.tar.gz": {"COPYING": "MIT\n"}}})
	p.add(model.ObjectID(trees[report.Head]), plannedPort{info: macports.PortInfo{Name: "jq", Version: "1.8.1"}, archives: map[string]map[string]string{"jq-1.8.1.tar.gz": {"COPYING": "GPL\n"}}})

	report, err = e.Review(t.Context(), 34905)
	require.NoError(t, err)
	require.Empty(t, report.UpstreamUnread)
	require.Len(t, report.Upstream, 1)
	require.Equal(t, []string{"upstream's COPYING changed; the Portfile's license line may need to follow", "Read COPYING"}, report.UpstreamWords(), "what was read is said too (batch 23)")
	require.Equal(t, []Dependent{{Name: "jaq", Directory: "textproc/jaq", On: []string{"jq"}, Phases: []string{"library"}}}, report.Dependents)
	markdown := report.Markdown()
	require.Contains(t, markdown, "What upstream's change means, comparing the source archives with the base's:\n- upstream's COPYING changed; the Portfile's license line may need to follow\n")
	require.Contains(t, markdown, "1 dependent, from the index at "+short(model.ObjectID(report.Base))+": jaq (library); candidates to look at.")
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		recorded, err := r.Assessments(store.AssessmentFilter{})
		require.Empty(t, recorded, "a review records no assessment")
		return err
	}))
}

// definedPorts names the ports textproc/jq defines, as an index with a
// subport there would, and reads no dependents.
type definedPorts struct{}

func (definedPorts) Dependents(context.Context, model.Source, []string) ([]Dependent, error) {
	return nil, nil
}

func (definedPorts) PortsDefined(context.Context, model.Source, []string) (map[string][]string, error) {
	return map[string][]string{"textproc/jq": {"jq", "jq-devel"}}, nil
}

// A review names each port the changed directory defines, as the base's
// index has them: #34620's "1 commit changing libuv", where devel/libuv
// also defines libuv-devel (the batch 11 run, batch 32).
func TestReviewNamesADirectorysOtherPorts(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	fake := f.withFork(t, e)
	contribution(t, f, fake)
	e.DependentReader = definedPorts{}

	report, err := e.Review(t.Context(), 34905)
	require.NoError(t, err)
	require.Equal(t, []string{"jq", "jq-devel"}, report.Ports)
	require.Contains(t, report.Summary(), "2 commits changing jq, jq-devel")
}
