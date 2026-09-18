package workflow_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"github.com/stretchr/testify/require"
)

// skipVerification turns a combined preparation into one that publishes
// without a build, the shape --skip-verify submits.
func skipVerification(request workflow.Request) workflow.Request {
	request.Spec.Build = nil
	request.Spec.Verification = record.VerificationSkipped
	return request
}

func TestCombinedUnverifiedPublicationDisclosesTheSkippedBuild(t *testing.T) {
	t.Parallel()
	for _, action := range []record.Action{record.Bump, record.BumpRevision} {
		t.Run(string(action), func(t *testing.T) {
			f, hosting, request := combinedFixture(t, action)
			request = skipVerification(request)
			id := submitPreparation(t, f, request)
			if action == record.Bump {
				f.run(t, id)
			}
			candidateJob(t, f, id)
			integrated := f.run(t, id) // Integrate the branch; no verification phase follows.
			require.Empty(t, integrated.Problems)
			status := f.status(t, id)
			require.Equal(t, record.PhasePublication, status.Jobs[0].Job.Phase, "%s: %s", status.Jobs[0].Job.State, status.Jobs[0].Job.Detail)
			require.Equal(t, record.JobActive, status.Jobs[0].Job.State)
			require.Contains(t, status.Jobs[0].Job.Detail, "verification skipped")
			require.Empty(t, status.Jobs[0].Attempts)
			f.run(t, id) // Freeze remote preconditions.
			status = f.status(t, id)
			require.Len(t, status.Jobs[0].Publications, 1)
			publication := status.Jobs[0].Publications[0]
			require.True(t, publication.Spec.Unverified)
			require.Empty(t, publication.Spec.EvidenceAttempt)
			require.Contains(t, publication.Spec.Desired.Body, "Not built locally")
			require.Contains(t, publication.Spec.Desired.Body, "--skip-verify")
			require.Contains(t, publication.Spec.Desired.Body, "[ ] Completed a full install (skipped at the author's request)")
			require.NotContains(t, publication.Spec.Desired.Body, "Verification attempt:")
			f.run(t, id) // Push.
			f.run(t, id) // Open the PR.
			f.run(t, id) // Confirm it.
			status = f.status(t, id)
			require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State)
			require.Equal(t, record.PublicationConfirmed, status.Jobs[0].Publications[0].State)
			require.Equal(t, 1, hosting.writes)
			require.Contains(t, hosting.observation.PullRequest.Body, "Not built locally")
			require.Empty(t, status.Jobs[0].Attempts)
			require.Zero(t, f.provider.count("submit"))
			rows := view.Project(status.Snapshot)
			require.Len(t, rows, 1)
			require.Equal(t, "published unverified", rows[0].State)
		})
	}
}

func TestUnverifiedPublicationRefusesBuildsDependentsAndKeptFailures(t *testing.T) {
	t.Parallel()
	f, _, request := combinedFixture(t, record.BumpRevision)
	base := skipVerification(request)
	_, err := f.engine.Submit(t.Context(), base)
	require.NoError(t, err)
	for _, mutate := range []func(*record.JobSpec){
		func(s *record.JobSpec) { s.Build = request.Spec.Build },
		func(s *record.JobSpec) { s.BuildRequirements = &record.BuildRequirements{Provider: "test"} },
		func(s *record.JobSpec) { s.IncludeDependents = true },
		func(s *record.JobSpec) { s.KeepFailed = true },
		func(s *record.JobSpec) { s.Destination = record.VerificationComplete },
	} {
		invalid := base
		invalid.ID = "request_other"
		mutate(&invalid.Spec)
		_, err := f.engine.Submit(t.Context(), invalid)
		require.ErrorIs(t, err, workflow.ErrInvalidRequest)
	}
}

func TestStandalonePublishCanSkipVerificationForTrackedContributionsOnly(t *testing.T) {
	t.Parallel()
	for _, tracked := range []bool{true, false} {
		t.Run(map[bool]string{true: "tracked", false: "untracked"}[tracked], func(t *testing.T) {
			f, hosting := publicationFixtureWithTracking(t, tracked)
			request, err := f.engine.BindPublication(t.Context(), workflow.PublicationRequest{ID: "unverified", Branch: "candidate", SkipVerify: true})
			if !tracked {
				require.ErrorIs(t, err, workflow.ErrInvalidRequest)
				require.ErrorContains(t, err, "not tracked")
				return
			}
			require.NoError(t, err)
			require.Nil(t, request.Spec.Build)
			require.Equal(t, record.VerificationSkipped, request.Spec.Verification)
			require.True(t, request.Spec.Publication.Unverified)
			require.Empty(t, request.Spec.Publication.EvidenceAttempt)
			require.Contains(t, request.Spec.Publication.Desired.Body, "Not built locally")
			receipt, err := f.engine.Submit(t.Context(), request)
			require.NoError(t, err)
			for range 4 {
				f.run(t, receipt.JobID)
			}
			status := f.status(t, receipt.JobID)
			require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State)
			require.Equal(t, record.PublicationConfirmed, status.Jobs[0].Publications[0].State)
			require.Equal(t, 1, hosting.writes)
		})
	}
}

func TestCorrectionCanSkipVerificationWithOrWithoutPublication(t *testing.T) {
	t.Parallel()
	for _, publishing := range []bool{false, true} {
		t.Run(map[bool]string{true: "publish", false: "branch"}[publishing], func(t *testing.T) {
			f, input := correctionFixture(t)
			input.ResolveBuild = nil
			input.SkipVerify = true
			if publishing {
				input.Publication = &publish.Options{}
			}
			bound, err := f.engine.BindCorrection(t.Context(), input)
			require.NoError(t, err)
			require.Nil(t, bound.Request.Spec.Build)
			require.Equal(t, record.VerificationSkipped, bound.Request.Spec.Verification)
			if publishing {
				require.Equal(t, record.Published, bound.Request.Spec.Destination)
				require.NotNil(t, bound.Request.Spec.PublishTo)
			} else {
				require.Equal(t, record.BranchReady, bound.Request.Spec.Destination)
				require.Nil(t, bound.Request.Spec.PublishTo)
			}
			receipt, err := f.engine.Submit(t.Context(), bound.Request)
			require.NoError(t, err)
			for range 6 {
				f.run(t, receipt.JobID)
			}
			status := f.status(t, receipt.JobID)
			require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State, "%s: %s", status.Jobs[0].Job.Phase, status.Jobs[0].Job.Detail)
			require.Empty(t, status.Jobs[0].Attempts)
			if publishing {
				require.Len(t, status.Jobs[0].Publications, 1)
				require.True(t, status.Jobs[0].Publications[0].Spec.Unverified)
			} else {
				require.Empty(t, status.Jobs[0].Publications)
			}
		})
	}
}
