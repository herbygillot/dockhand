package workflow_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func correctionFixture(t *testing.T) (*fixture, workflow.CorrectionRequest) {
	f, _ := publicationFixture(t)
	for key, value := range map[string]string{"user.name": "Fixture", "user.email": "fixture@example.invalid"} {
		out, err := exec.CommandContext(t.Context(), "git", "-C", f.repo.Root, "config", key, value).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	f.engine.Provider = f.provider
	request := workflow.CorrectionRequest{ID: "correction", Action: record.Amend, Branch: "candidate", Platform: buildPlatform, ResolveBuild: func(context.Context, macports.Snapshot) (workflow.BuildResolution, error) {
		build := *f.request("").Spec.Build
		build.VerifierDigest = "fixture:v1"
		return workflow.BuildResolution{Build: &build}, nil
	}}
	return f, request
}

func TestCorrectionPreservesChangeAndReusesSameTree(t *testing.T) {
	t.Parallel()
	f, input := correctionFixture(t)
	bound, err := f.engine.BindCorrection(t.Context(), input)
	require.NoError(t, err)
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	retry, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	require.Equal(t, receipt, retry)
	for range 4 {
		f.run(t, receipt.JobID)
	}
	result := f.status(t, receipt.JobID)
	require.Equal(t, record.JobCompleted, result.Jobs[0].Job.State, result.Jobs[0].Job.Detail)
	require.Equal(t, record.ChangeID("change"), result.Jobs[0].Job.ChangeID)
	require.NotEmpty(t, result.Jobs[0].Job.ReusedAttempt)
	require.Equal(t, 1, f.provider.count("submit"), "only the original fixture verification should run")
	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, r state.Reader) error {
		change, err := r.Change(ctx, "change")
		require.NoError(t, err)
		revision, err := r.Revision(ctx, change.CurrentRevision)
		require.NoError(t, err)
		require.Equal(t, record.RevisionID("publication_revision"), revision.Previous)
		require.Equal(t, f.source.Tree, revision.Source.Tree)
		return nil
	}))
}

func TestCorrectionRecoveryAndBranchMove(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"already-replaced", "moved", "concurrent-adoption"} {
		t.Run(mode, func(t *testing.T) {
			f, input := correctionFixture(t)
			bound, err := f.engine.BindCorrection(t.Context(), input)
			require.NoError(t, err)
			receipt, err := f.engine.Submit(t.Context(), bound.Request)
			require.NoError(t, err)
			f.run(t, receipt.JobID)
			candidate := f.status(t, receipt.JobID).Jobs[0].Job.Prepared
			require.NotNil(t, candidate)
			if mode == "concurrent-adoption" {
				verification, err := f.engine.BindVerification(t.Context(), bindRequest(f, "other"))
				require.NoError(t, err)
				_, err = f.engine.Submit(t.Context(), verification.Request)
				require.ErrorContains(t, err, "correction")
			} else if mode == "moved" {
				commitPort(t, f, "candidate", "version 9\n")
			} else {
				require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: string(f.source.Commit)}, Desired: git.RefValue{Exists: true, Object: string(candidate.Source.Commit)}}}))
				require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
					job, err := tx.Job(ctx, receipt.JobID)
					if err != nil {
						return err
					}
					job.Prepared.IntegrationStarted = true
					return tx.PutJob(ctx, job)
				}))
			}
			f.run(t, receipt.JobID)
			job := f.status(t, receipt.JobID).Jobs[0].Job
			if mode == "moved" {
				require.Equal(t, record.JobNeedsAttention, job.State)
				require.Empty(t, job.ResultRevision)
			} else {
				require.NotEmpty(t, job.ResultRevision)
			}
		})
	}
}

func TestCorrectionRefusesStaleRevisionAndPendingWork(t *testing.T) {
	t.Parallel()
	f, input := correctionFixture(t)
	bound, err := f.engine.BindCorrection(t.Context(), input)
	require.NoError(t, err)
	verification, err := f.engine.BindVerification(t.Context(), bindRequest(f, "other"))
	require.NoError(t, err)
	_, err = f.engine.Submit(t.Context(), verification.Request)
	require.NoError(t, err)
	_, err = f.engine.Submit(t.Context(), bound.Request)
	require.Error(t, err)
}

func TestReassociatePreservesPRRemoteBranch(t *testing.T) {
	t.Parallel()
	f, hosting := publicationFixture(t)
	id := submitPublication(t, f, "publish-first")
	for range 5 {
		f.run(t, id)
	}
	require.Equal(t, record.JobCompleted, f.status(t, id).Jobs[0].Job.State)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/renamed", Desired: git.RefValue{Exists: true, Object: string(f.source.Commit)}}, {Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: string(f.source.Commit)}}}))
	change, err := f.engine.Reassociate(t.Context(), "change", "renamed", buildPlatform)
	require.NoError(t, err)
	require.Equal(t, "renamed", change.Branch)
	require.NotEmpty(t, change.PullRequestID)
	request, err := f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "again", Branch: "renamed"})
	require.NoError(t, err)
	require.Equal(t, "renamed", request.Spec.Publication.LocalBranch)
	require.Equal(t, "candidate", request.Spec.Publication.HeadBranch)
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	for range 4 {
		f.run(t, receipt.JobID)
	}
	require.Equal(t, record.JobCompleted, f.status(t, receipt.JobID).Jobs[0].Job.State)
	require.Equal(t, 1, hosting.writes, "unchanged PR must not be recreated")
}

func TestCorrectionRetriesOriginalBranchAfterInterruptedIntegration(t *testing.T) {
	t.Parallel()
	f, input := correctionFixture(t)
	bound, err := f.engine.BindCorrection(t.Context(), input)
	require.NoError(t, err)
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	f.run(t, receipt.JobID)
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		job, err := tx.Job(ctx, receipt.JobID)
		if err != nil {
			return err
		}
		job.Prepared.IntegrationStarted = true
		return tx.PutJob(ctx, job)
	}))
	f.advance(time.Hour)
	f.run(t, receipt.JobID)
	require.NotEmpty(t, f.status(t, receipt.JobID).Jobs[0].Job.ResultRevision)
}

func TestChangedCorrectionBuildsAgain(t *testing.T) {
	t.Parallel()
	f, input := correctionFixture(t)
	commitPort(t, f, "candidate", "version 3\n")
	bound, err := f.engine.BindCorrection(t.Context(), input)
	require.NoError(t, err)
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	for range 4 {
		result := f.run(t, receipt.JobID)
		require.Empty(t, result.Problems)
	}
	job := f.status(t, receipt.JobID).Jobs[0].Job
	require.Empty(t, job.ReusedAttempt)
	require.Equal(t, 2, f.provider.count("submit"))
	require.NotEqual(t, f.source.Tree, f.attempt(t, receipt.JobID).Spec.Source.Tree)
	require.Equal(t, f.source.Commit, f.attempt(t, receipt.JobID).Spec.ReplaceRemoteHead, "remote replacement authority survives SQLite")
	f.provider.observe = terminal(f, record.VerdictPassed)
	f.run(t, receipt.JobID)
	require.Equal(t, record.JobCompleted, f.status(t, receipt.JobID).Jobs[0].Job.State)
}

func TestCorrectionUpdatesExistingPRAndPreservesHumanBody(t *testing.T) {
	t.Parallel()
	f, input := correctionFixture(t)
	hosting := f.engine.PullRequests.(*publicationForge)
	first := submitPublication(t, f, "first-pr")
	for range 4 {
		f.run(t, first)
	}
	require.Equal(t, record.JobCompleted, f.status(t, first).Jobs[0].Job.State)
	hosting.observation.PullRequest.Body = "human edited PR body"
	input.Publication = &publish.Options{}
	bound, err := f.engine.BindCorrection(t.Context(), input)
	require.NoError(t, err)
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	for range 8 {
		result := f.run(t, receipt.JobID)
		require.Empty(t, result.Problems)
	}
	result := f.status(t, receipt.JobID)
	require.Equal(t, record.JobCompleted, result.Jobs[0].Job.State, result.Jobs[0].Job.Detail)
	require.Equal(t, 2, hosting.writes, "the head replacement, and one write to give the hand-written body its Tested on section")
	require.True(t, strings.HasPrefix(result.PullRequests[0].Body, "human edited PR body\n\n###### Tested on"), "the person's words stay first: %q", result.PullRequests[0].Body)
	require.EqualValues(t, 1, result.PullRequests[0].Ref.Number)
	require.Equal(t, result.Jobs[0].Job.Prepared.Source.Commit, result.PullRequests[0].RemoteHead)
}

func TestExistingPRRejectsUnexpectedRemoteHeadDuringPlanning(t *testing.T) {
	t.Parallel()
	f, hosting := publicationFixture(t)
	id := submitPublication(t, f, "first")
	for range 4 {
		f.run(t, id)
	}
	sig := git.Signature{Name: "Other", Email: "other@example.invalid", When: f.now()}
	other, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: string(f.source.Tree), Parents: []string{string(f.source.Base)}, Message: "someone else's change", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, f.repo.Push(t.Context(), git.Push{Remote: hosting.remote, Branch: "candidate", Commit: other, ExpectedRemote: git.RefValue{Exists: true, Object: string(f.source.Commit)}}))
	_, err = f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "again", Branch: "candidate", Adopt: true})
	require.ErrorContains(t, err, "head changed remotely")
}

func TestCorrectionExplicitSubjectPreservesBody(t *testing.T) {
	t.Parallel()
	f, input := correctionFixture(t)
	input.Subject = "corrective update"
	bound, err := f.engine.BindCorrection(t.Context(), input)
	require.NoError(t, err)
	message, err := f.repo.CommitMessage(t.Context(), string(bound.Request.Spec.Preparation.Correction.Candidate.Commit))
	require.NoError(t, err)
	require.Equal(t, "fixture: corrective update\n\nContribution details", message)
	input.Subject = "bad\nsubject"
	_, err = f.engine.BindCorrection(t.Context(), input)
	require.ErrorContains(t, err, "one nonempty line")
	input.Subject = "fixture: corrective update"
	_, err = f.engine.BindCorrection(t.Context(), input)
	require.ErrorContains(t, err, "already begins with")
}

func TestCorrectionCitesTicketsOnceAndStillReusesEvidence(t *testing.T) {
	t.Parallel()
	f, input := correctionFixture(t)
	closes := record.Reference{Relation: record.ReferenceCloses, URL: "https://trac.macports.org/ticket/74379"}
	see := record.Reference{Relation: record.ReferenceSee, URL: "https://trac.macports.org/ticket/74422"}
	input.References = []record.Reference{closes, see, closes}
	bound, err := f.engine.BindCorrection(t.Context(), input)
	require.NoError(t, err)
	message, err := f.repo.CommitMessage(t.Context(), string(bound.Request.Spec.Preparation.Correction.Candidate.Commit))
	require.NoError(t, err)
	require.Equal(t, "fixture: update to 2\n\nContribution details\n\nCloses: https://trac.macports.org/ticket/74379\nSee: https://trac.macports.org/ticket/74422", message)
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	for range 4 {
		f.run(t, receipt.JobID)
	}
	result := f.status(t, receipt.JobID)
	require.Equal(t, record.JobCompleted, result.Jobs[0].Job.State, result.Jobs[0].Job.Detail)
	require.NotEmpty(t, result.Jobs[0].Job.ReusedAttempt, "a message-only amendment keeps the tree and its evidence")
	require.Equal(t, 1, f.provider.count("submit"))

	input.References = []record.Reference{{Relation: "fixes", URL: "https://trac.macports.org/ticket/1"}}
	_, err = f.engine.BindCorrection(t.Context(), input)
	require.ErrorContains(t, err, "invalid reference")
}

func TestVerificationAfterRenameKeepsThePRHeadAsItsRemoteBranch(t *testing.T) {
	t.Parallel()
	f, _ := publicationFixture(t)
	id := submitPublication(t, f, "publish-first")
	for range 5 {
		f.run(t, id)
	}
	require.Equal(t, record.JobCompleted, f.status(t, id).Jobs[0].Job.State)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/renamed", Desired: git.RefValue{Exists: true, Object: string(f.source.Commit)}}, {Name: "refs/heads/candidate", Expected: git.RefValue{Exists: true, Object: string(f.source.Commit)}}}))
	change, err := f.engine.Reassociate(t.Context(), "change", "renamed", buildPlatform)
	require.NoError(t, err)
	require.Equal(t, "renamed", change.Branch)
	request := f.request("verify-renamed")
	request.Spec.SourceBranch = "renamed"
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	for range 3 {
		f.run(t, receipt.JobID)
	}
	attempt := f.attempt(t, receipt.JobID)
	require.Equal(t, "renamed", attempt.Spec.Branch, "the local locator captures the commit")
	require.Equal(t, "candidate", attempt.Spec.RemoteBranch, "the fork branch stays the PR head")
	require.Equal(t, "candidate", attempt.Spec.PushBranch())
}
