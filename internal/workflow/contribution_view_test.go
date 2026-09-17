package workflow

import (
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestProjectGroupsJobsByContributionAndWordsEachPhase(t *testing.T) {
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	platform := record.Platform{OS: "macOS", Version: "26", Architecture: "arm64"}
	finished := base.Add(10 * time.Minute)
	status := Status{
		Changes: []record.Change{
			{ID: "change_jq", InitiatingTarget: "jq", Branch: "dockhand/bump/jq", Targets: []record.Target{{Name: "jq"}}, Disposition: record.ChangeOpen, PullRequestID: "pr_jq", CreatedAt: base},
			{ID: "change_gh", InitiatingTarget: "gh", Branch: "dockhand/bump/gh", Disposition: record.ChangeMerged, CreatedAt: base.Add(-time.Hour)},
		},
		PullRequests: []record.PullRequest{{ID: "pr_jq", ChangeID: "change_jq", State: record.PullRequestOpen, ObservedAt: finished, Ref: record.PullRequestRef{URL: "https://github.com/macports/macports-ports/pull/1"},
			Status: &record.PullRequestStatus{Mergeable: "yes", Review: "none", Checks: record.CheckSummary{Total: 4, Passed: 1, Pending: 3}}}},
		Jobs: []JobStatus{
			{Job: record.Job{ID: "job_bump", ChangeID: "change_jq", State: record.JobCompleted, Phase: record.PhaseVerification, AcceptedAt: base, FinishedAt: &finished,
				Spec:            record.JobSpec{Action: record.Bump, Destination: record.VerificationComplete, Targets: []record.Target{{Name: "jq"}}},
				ResolvedRelease: &record.Release{Selection: record.Selection{CurrentVersion: "1.7"}, Version: "1.8.1"}, Prepared: &record.PreparedChange{Branch: "dockhand/bump/jq"}, ResultRevision: "revision"}},
			{Job: record.Job{ID: "job_publish", ChangeID: "change_jq", State: record.JobCompleted, Phase: record.PhasePublication, AcceptedAt: base.Add(time.Minute), FinishedAt: &finished,
				Spec: record.JobSpec{Action: record.Publish, Destination: record.Published, Targets: []record.Target{{Name: "jq"}}}},
				Publications: []record.PublicationAction{{State: record.PublicationConfirmed}}},
			{Job: record.Job{ID: "job_verify", State: record.JobActive, Phase: record.PhaseVerification, AcceptedAt: base.Add(2 * time.Minute), Detail: "Verification running",
				Spec: record.JobSpec{Action: record.Verify, Destination: record.VerificationComplete, Targets: []record.Target{{Name: "deno"}}}},
				Attempts: []record.Attempt{{State: record.AttemptRunning, Spec: record.BuildSpec{Config: record.BuildConfig{Platform: platform}}}}},
		},
	}
	rows := Project(status)
	require.Len(t, rows, 3)
	jq := rows[0]
	require.Equal(t, "jq", jq.Port)
	require.Equal(t, "1.7 -> 1.8.1", jq.Change)
	require.Equal(t, "dockhand/bump/jq", jq.Branch)
	require.Equal(t, "publication", jq.Phase)
	require.Equal(t, "published", jq.State)
	require.Equal(t, "PR open, 3 checks pending", jq.Next)
	require.Equal(t, "https://github.com/macports/macports-ports/pull/1", jq.PullRequest)
	require.Nil(t, jq.Active)
	require.Len(t, jq.History, 2)
	require.Equal(t, record.JobID("job_bump"), jq.History[0].JobID)
	require.False(t, jq.Retired)

	deno := rows[1]
	require.Equal(t, "deno", deno.Port)
	require.Equal(t, "verification", deno.Change)
	require.Equal(t, "building on macOS 26", deno.State)
	require.Equal(t, "verification pending", deno.Next)
	require.NotNil(t, deno.Active)
	require.Equal(t, "Verification running", deno.Active.Detail)
	require.Empty(t, deno.ChangeID)
	require.False(t, deno.Retired, "a running standalone verification is current")

	gh := rows[2]
	require.Equal(t, "gh", gh.Port)
	require.Equal(t, "done", gh.Phase)
	require.Equal(t, "merged", gh.State)
	require.Equal(t, "merged; branches cleaned", gh.Next)
	require.True(t, gh.Retired)
	require.Equal(t, "update", gh.Change, "a merged change without jobs in the snapshot still has a row")
}

func TestProjectWordsWaitingFailureAndAttention(t *testing.T) {
	platform := record.Platform{OS: "macOS", Version: "15"}
	waiting := JobStatus{Job: record.Job{ID: "job_w", ChangeID: "c1", State: record.JobActive, Phase: record.PhaseVerification, Spec: record.JobSpec{Action: record.Bump, Destination: record.Published, Targets: []record.Target{{Name: "a"}}}},
		Attempts: []record.Attempt{{State: record.AttemptQueued, Spec: record.BuildSpec{Config: record.BuildConfig{Platform: platform}}}}}
	failed := JobStatus{Job: record.Job{ID: "job_f", ChangeID: "c2", State: record.JobFailed, Phase: record.PhaseVerification, Spec: record.JobSpec{Action: record.BumpRevision, Targets: []record.Target{{Name: "b"}}}},
		Attempts: []record.Attempt{{State: record.AttemptFinished, Evidence: &record.Evidence{Verdict: record.VerdictFailed, Failure: &record.Failure{Phase: "build"}}}}}
	attention := JobStatus{Job: record.Job{ID: "job_a", ChangeID: "c3", State: record.JobNeedsAttention, Phase: record.PhasePreparation, Spec: record.JobSpec{Action: record.Bump, Targets: []record.Target{{Name: "c"}}},
		Prepared: &record.PreparedChange{Branch: "b", PatchProblems: []string{"p1: rejects 4 hunks", "p2: rejects 1 hunk"}}}}
	ready := JobStatus{Job: record.Job{ID: "job_r", ChangeID: "c4", State: record.JobCompleted, Phase: record.PhasePreparation, ResultRevision: "r", Spec: record.JobSpec{Action: record.Bump, Destination: record.BranchReady, Targets: []record.Target{{Name: "d"}}}}}
	stopped := JobStatus{Job: record.Job{ID: "job_s", ChangeID: "c5", State: record.JobNeedsAttention, Phase: record.PhasePreparation, Detail: "forge: authentication is required", Spec: record.JobSpec{Action: record.Bump, Targets: []record.Target{{Name: "e"}}}}}
	rows := Project(Status{Jobs: []JobStatus{waiting, failed, attention, ready, stopped}})
	byPort := map[string]Contribution{}
	for _, row := range rows {
		byPort[row.Port] = row
	}
	require.Equal(t, "waiting for capacity", byPort["a"].State)
	require.Equal(t, "publication pending", byPort["a"].Next)
	require.Equal(t, "failed", byPort["b"].State)
	require.Equal(t, "build failed; fix and amend, or abandon", byPort["b"].Next)
	require.Equal(t, "needs attention", byPort["c"].State)
	require.Equal(t, "patches no longer apply (2); refresh them and amend", byPort["c"].Next)
	require.Equal(t, "branch ready", byPort["d"].State)
	require.Equal(t, "verify when ready: dockhand verify d", byPort["d"].Next)
	require.Equal(t, "bump again once fixed, or abandon: forge: authentication is required", byPort["e"].Next, "a preparation that stopped before a branch is retried by bumping again")
}

func TestProjectFoldsAPortsContributionsUnderItsOpenOne(t *testing.T) {
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	status := Status{
		Changes: []record.Change{
			{ID: "c_old", InitiatingTarget: "wasmer", Disposition: record.ChangeClosed, CreatedAt: base.Add(-2 * time.Hour)},
			{ID: "c_merged", InitiatingTarget: "wasmer", Branch: "dockhand/bump/wasmer-1", Disposition: record.ChangeMerged, CreatedAt: base.Add(-time.Hour)},
			{ID: "c_open", InitiatingTarget: "wasmer", Branch: "dockhand/bump/wasmer-2", Disposition: record.ChangeOpen, CreatedAt: base},
		},
		Jobs: []JobStatus{
			{Job: record.Job{ID: "j_old", ChangeID: "c_old", State: record.JobNeedsAttention, Phase: record.PhasePreparation, AcceptedAt: base.Add(-2 * time.Hour), Detail: "forge: authentication is required", Spec: record.JobSpec{Action: record.Bump, Targets: []record.Target{{Name: "wasmer"}}}}},
			{Job: record.Job{ID: "j_open", ChangeID: "c_open", State: record.JobCompleted, Phase: record.PhaseVerification, AcceptedAt: base, ResultRevision: "r", Spec: record.JobSpec{Action: record.Bump, Destination: record.VerificationComplete, Targets: []record.Target{{Name: "wasmer"}}}, ResolvedRelease: &record.Release{Selection: record.Selection{CurrentVersion: "7.4.0"}, Version: "7.4.1"}, Prepared: &record.PreparedChange{Branch: "dockhand/bump/wasmer-2"}},
				Attempts: []record.Attempt{{State: record.AttemptFinished, Evidence: &record.Evidence{Verdict: record.VerdictPassed}}}},
			{Job: record.Job{ID: "j_verify", State: record.JobCompleted, Phase: record.PhaseVerification, AcceptedAt: base.Add(time.Minute), Spec: record.JobSpec{Action: record.Verify, Destination: record.VerificationComplete, Targets: []record.Target{{Name: "wasmer"}}}},
				Attempts: []record.Attempt{{State: record.AttemptFinished, Evidence: &record.Evidence{Verdict: record.VerdictPassed}}}},
		},
	}
	rows := Project(status)
	require.Len(t, rows, 1, "one row per port")
	row := rows[0]
	require.Equal(t, "wasmer", row.Port)
	require.Equal(t, record.ChangeID("c_open"), row.ChangeID, "the open contribution leads even when a standalone verification is newer")
	require.Equal(t, "7.4.0 -> 7.4.1", row.Change)
	require.Equal(t, "verified", row.State)
	require.Len(t, row.Earlier, 3)
	require.Equal(t, "verification", row.Earlier[0].Change, "newest first")
	require.True(t, row.Earlier[0].Retired, "a finished standalone verification is history")
	require.Equal(t, "merged", row.Earlier[1].State)
	require.Equal(t, "retired", row.Earlier[2].State)
	require.Equal(t, "stopped before a branch; bump again once fixed: forge: authentication is required", row.Earlier[2].Next)
	require.True(t, row.Earlier[2].Retired)
}

func TestFinishedStandaloneVerificationsRetire(t *testing.T) {
	verified := JobStatus{Job: record.Job{ID: "j", State: record.JobCompleted, Phase: record.PhaseVerification, Spec: record.JobSpec{Action: record.Verify, Destination: record.VerificationComplete, Targets: []record.Target{{Name: "libmd"}}}},
		Attempts: []record.Attempt{{State: record.AttemptFinished, Evidence: &record.Evidence{Verdict: record.VerdictPassed}}}}
	rows := Project(Status{Jobs: []JobStatus{verified}})
	require.Len(t, rows, 1)
	require.Equal(t, "verified", rows[0].State)
	require.True(t, rows[0].Retired, "there is nothing to abandon or continue; it hides with the retired rows")
	require.Empty(t, Current(rows))
}
