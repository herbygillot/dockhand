package workflow_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestAdoptTracksOneCommitAboveMasterInOnePortDirectory(t *testing.T) {
	t.Parallel()
	f, _ := manualPublicationFixture(t)
	request := workflow.AdoptRequest{Branch: "candidate", Upstream: f.source.Base, Platform: buildPlatform, DryRun: true}
	dry, err := f.engine.AdoptContribution(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "devel/fixture/Portfile", dry.Portfile)
	require.Equal(t, "fixture", dry.Change.InitiatingTarget, "the port is inferred from the directory the branch changes")
	require.Contains(t, dry.Detail, "nothing recorded")
	_, err = f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Branch: "candidate"})
	require.ErrorIs(t, err, state.ErrNotFound, "a dry run records nothing")

	request.DryRun = false
	result, err := f.engine.AdoptContribution(t.Context(), request)
	require.NoError(t, err)
	change, err := f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Branch: "candidate"})
	require.NoError(t, err)
	require.Equal(t, result.Change.ID, change.ID)
	require.Equal(t, "candidate", change.Branch)
	require.Equal(t, []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}, change.Targets)
	require.Equal(t, f.source, result.Revision.Source, "the revision is the branch's commit above its master base")
	require.Contains(t, result.Detail, "verify fixture builds it")
	_, err = f.engine.AdoptContribution(t.Context(), request)
	require.ErrorContains(t, err, "already tracked as the contribution for fixture")
	request.Branch = "no-such-branch"
	_, err = f.engine.AdoptContribution(t.Context(), request)
	require.Error(t, err)
}

func TestAdoptRefusesSeveralCommitsAndSeveralDirectories(t *testing.T) {
	t.Parallel()
	f, _ := manualPublicationFixture(t)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: f.now()}
	stacked, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: string(f.source.Tree), Parents: []string{string(f.source.Commit)}, Message: "fixture: second thoughts", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/stacked", Desired: git.RefValue{Exists: true, Object: stacked}}}))
	_, err = f.engine.AdoptContribution(t.Context(), workflow.AdoptRequest{Branch: "stacked", Upstream: f.source.Base, Platform: buildPlatform})
	require.ErrorContains(t, err, "2 commits above master")

	blob, err := f.repo.WriteBlob(t.Context(), []byte("version 3\n"))
	require.NoError(t, err)
	port, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Object: blob, Type: "blob", Mode: 0100644}})
	require.NoError(t, err)
	category, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "fixture", Object: port, Type: "tree", Mode: 040000}, {Name: "other", Object: port, Type: "tree", Mode: 040000}})
	require.NoError(t, err)
	tree, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "devel", Object: category, Type: "tree", Mode: 040000}})
	require.NoError(t, err)
	wide, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{string(f.source.Base)}, Message: "two ports", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/wide", Desired: git.RefValue{Exists: true, Object: wide}}}))
	_, err = f.engine.AdoptContribution(t.Context(), workflow.AdoptRequest{Branch: "wide", Upstream: f.source.Base, Platform: buildPlatform})
	require.ErrorContains(t, err, "one port directory")
}

func TestAdoptSquashFoldsAStackedBranchAndKeepsTheOriginals(t *testing.T) {
	t.Parallel()
	f, _ := manualPublicationFixture(t)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: f.now()}
	blob, err := f.repo.WriteBlob(t.Context(), []byte("version 2\nrevision 1\n"))
	require.NoError(t, err)
	port, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Object: blob, Type: "blob", Mode: 0100644}})
	require.NoError(t, err)
	category, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "fixture", Object: port, Type: "tree", Mode: 040000}})
	require.NoError(t, err)
	tree, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "devel", Object: category, Type: "tree", Mode: 040000}})
	require.NoError(t, err)
	stacked, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{string(f.source.Commit)}, Message: "fixture: second thoughts", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/stacked", Desired: git.RefValue{Exists: true, Object: stacked}}}))

	request := workflow.AdoptRequest{Branch: "stacked", Upstream: f.source.Base, Platform: buildPlatform, Squash: true, DryRun: true}
	dry, err := f.engine.AdoptContribution(t.Context(), request)
	require.NoError(t, err)
	require.Contains(t, dry.Detail, "Would squash the 2 commits")
	head, _, err := f.repo.Branch(t.Context(), "stacked")
	require.NoError(t, err)
	require.Equal(t, stacked, head, "a dry run moves nothing")

	request.DryRun = false
	result, err := f.engine.AdoptContribution(t.Context(), request)
	require.NoError(t, err)
	head, headTree, err := f.repo.Branch(t.Context(), "stacked")
	require.NoError(t, err)
	require.Equal(t, string(result.Revision.Source.Commit), head)
	require.Equal(t, tree, headTree, "the fold carries the branch's tree")
	parent, err := f.repo.SingleParent(t.Context(), head)
	require.NoError(t, err)
	require.Equal(t, string(f.source.Base), parent, "one commit on the master base")
	message, err := f.repo.CommitMessage(t.Context(), head)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(message, "fixture: update to 2"), "the oldest commit's message: %q", message)
	backup, err := f.repo.ReadRef(t.Context(), "refs/dockhand/adopted/stacked")
	require.NoError(t, err)
	require.Equal(t, git.RefValue{Exists: true, Object: stacked}, backup, "the originals stay reachable")
}

func pullRequestFixture(t *testing.T, f *fixture, hosting *publicationForge, head record.ObjectID, headRepository, headBranch string) record.PullRequestRef {
	t.Helper()
	ref := record.PullRequestRef{Forge: "fixture", Repository: "author/ports", Number: 7, URL: "https://example.invalid/pull/7"}
	hosting.observation = forge.PullRequestObservation{Found: true, ObservedAt: f.now(), PullRequest: record.PullRequest{Ref: ref, HeadRepository: headRepository, HeadBranch: headBranch, BaseBranch: "main", State: record.PullRequestOpen, RemoteHead: head, Title: "fixture: update to 2", Body: "I wrote this myself."}}
	return ref
}

func TestAdoptPullRequestFromOwnForkReusesTheLocalBranchAndAttachesThePR(t *testing.T) {
	t.Parallel()
	f, hosting := manualPublicationFixture(t)
	require.NoError(t, f.repo.Push(t.Context(), git.Push{Remote: hosting.remote, Branch: "candidate", Commit: string(f.source.Commit)}))
	ref := pullRequestFixture(t, f, hosting, f.source.Commit, "author/ports", "candidate")
	result, err := f.engine.AdoptContribution(t.Context(), workflow.AdoptRequest{PullRequest: &ref, Upstream: f.source.Base, Platform: buildPlatform, KeepBody: true})
	require.NoError(t, err)
	require.Equal(t, "candidate", result.Change.Branch, "the person's own fork keeps the pull request's branch name")
	require.NotNil(t, result.PullRequest)
	change, err := f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Target: "fixture"})
	require.NoError(t, err)
	require.Equal(t, result.PullRequest.ID, change.PullRequestID)
	require.Equal(t, change.CurrentRevision, change.PublishedRevision, "the pull request's head is what is published")
	require.True(t, change.KeepBody)
	require.Contains(t, result.Detail, "amend fixture revises it and updates the pull request")

	_, err = f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "refresh", Branch: "candidate", Options: publish.Options{RefreshBody: true}})
	require.ErrorContains(t, err, "adopted with --keep-body")
}

func TestAdoptStackedPullRequestThenSquashFoldsItUnderThePRTitle(t *testing.T) {
	t.Parallel()
	f, hosting := manualPublicationFixture(t)
	sig := git.Signature{Name: "Contributor", Email: "contributor@example.invalid", When: f.now()}
	second := commitPortOnto(t, f, f.source.Commit, "version 2\nrevision 1\n", "oops", sig)
	third := commitPortOnto(t, f, record.ObjectID(second), "version 2\nrevision 2\n", "fix again", sig)
	require.NoError(t, f.repo.Push(t.Context(), git.Push{Remote: hosting.remote, Branch: "feature", Commit: third}))
	ref := pullRequestFixture(t, f, hosting, record.ObjectID(third), "someone/ports", "feature")
	result, err := f.engine.AdoptContribution(t.Context(), workflow.AdoptRequest{PullRequest: &ref, Upstream: f.source.Base, Platform: buildPlatform})
	require.NoError(t, err)
	require.Equal(t, "pr/7", result.Change.Branch, "someone else's head goes under a name that says so")
	require.Equal(t, 3, result.Commits)
	require.Equal(t, f.source.Base, result.Revision.Source.Base, "a stacked head is recorded on its merge base")
	require.Contains(t, result.Detail, "amend fixture --squash")

	input := workflow.CorrectionRequest{ID: "fold", Action: record.Amend, Target: "fixture", Squash: true, Platform: buildPlatform, SkipVerify: true}
	bound, err := f.engine.BindCorrection(t.Context(), input)
	require.NoError(t, err)
	candidate := bound.Request.Spec.Preparation.Correction.Candidate
	parent, err := f.repo.SingleParent(t.Context(), string(candidate.Commit))
	require.NoError(t, err)
	require.Equal(t, string(f.source.Base), parent, "one commit on the base")
	_, headTree, err := f.repo.Branch(t.Context(), "pr/7")
	require.NoError(t, err)
	require.Equal(t, record.ObjectID(headTree), candidate.Tree, "the fold carries the head's tree")
	require.Equal(t, "fixture: update to 2", bound.Message, "the pull request's title is the subject")
	input.Message = "fixture: update to 2\n\nEdited by hand.\n"
	bound, err = f.engine.BindCorrection(t.Context(), input)
	require.NoError(t, err)
	message, err := f.repo.CommitMessage(t.Context(), string(bound.Request.Spec.Preparation.Correction.Candidate.Commit))
	require.NoError(t, err)
	require.Equal(t, "fixture: update to 2\n\nEdited by hand.", message)
}

func commitPortOnto(t *testing.T, f *fixture, parent record.ObjectID, contents, message string, sig git.Signature) string {
	t.Helper()
	blob, err := f.repo.WriteBlob(t.Context(), []byte(contents))
	require.NoError(t, err)
	port, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Object: blob, Type: "blob", Mode: 0100644}})
	require.NoError(t, err)
	category, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "fixture", Object: port, Type: "tree", Mode: 040000}})
	require.NoError(t, err)
	tree, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "devel", Object: category, Type: "tree", Mode: 040000}})
	require.NoError(t, err)
	commit, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{string(parent)}, Message: message, Author: sig, Committer: sig})
	require.NoError(t, err)
	return commit
}

func TestSquashOfSomeoneElsesPullRequestPushesToTheirForkAndLetsTheForgeDecide(t *testing.T) {
	t.Parallel()
	f, hosting := manualPublicationFixture(t)
	sig := git.Signature{Name: "Contributor", Email: "contributor@example.invalid", When: f.now()}
	second := commitPortOnto(t, f, f.source.Commit, "version 2\nrevision 1\n", "oops", sig)
	fork := filepath.Join(t.TempDir(), "fork.git")
	out, err := exec.CommandContext(t.Context(), "git", "init", "--bare", "-q", fork).CombinedOutput()
	require.NoError(t, err, "%s", out)
	hosting.forkRemote, hosting.headName = fork, "someone/ports"
	require.NoError(t, f.repo.Push(t.Context(), git.Push{Remote: fork, Branch: "feature", Commit: second}))
	ref := pullRequestFixture(t, f, hosting, record.ObjectID(second), "someone/ports", "feature")
	_, err = f.engine.AdoptContribution(t.Context(), workflow.AdoptRequest{PullRequest: &ref, Upstream: f.source.Base, Platform: buildPlatform})
	require.NoError(t, err)
	bound, err := f.engine.BindCorrection(t.Context(), workflow.CorrectionRequest{ID: "fold", Action: record.Amend, Target: "fixture", Squash: true, Platform: buildPlatform, SkipVerify: true, Publication: &publish.Options{}})
	require.NoError(t, err, "no ownership check stands between a reviewer and a contributor's pull request")
	require.Equal(t, "someone/ports", bound.Request.Spec.PublishTo.HeadRepository)
	require.Equal(t, fork, bound.Request.Spec.PublishTo.PushURL, "the push goes to the pull request's fork")
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	for range 8 {
		f.run(t, receipt.JobID)
	}
	status := f.status(t, receipt.JobID)
	require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State, status.Jobs[0].Job.Detail)
	remote, err := f.repo.RemoteHead(t.Context(), fork, "feature")
	require.NoError(t, err)
	require.Equal(t, bound.Request.Spec.Preparation.Correction.Candidate.Commit, record.ObjectID(remote.Object), "the fork's branch now holds the one folded commit")
}
