package view

import (
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestProjectGroupsJobsByContributionAndWordsEachPhase(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	platform := record.Platform{OS: "macOS", Version: "26", Architecture: "arm64"}
	finished := base.Add(10 * time.Minute)
	status := Snapshot{
		Changes: []record.Change{
			{ID: "change_jq", InitiatingTarget: "jq", Branch: "dockhand/bump/jq", Targets: []record.Target{{Name: "jq"}}, Disposition: record.ChangeOpen, PullRequestID: "pr_jq", CreatedAt: base},
			{ID: "change_gh", InitiatingTarget: "gh", Branch: "dockhand/bump/gh", Disposition: record.ChangeMerged, CreatedAt: base.Add(-time.Hour),
				Cleanup: &record.BranchCleanup{Local: record.CleanupOutcome{Name: "dockhand/bump/gh", State: record.CleanupComplete}, Fork: record.CleanupOutcome{Name: "author/ports:dockhand/bump/gh", State: record.CleanupComplete}}},
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
	t.Parallel()
	platform := record.Platform{OS: "macOS", Version: "15"}
	waiting := JobStatus{Job: record.Job{ID: "job_w", ChangeID: "c1", State: record.JobActive, Phase: record.PhaseVerification, Spec: record.JobSpec{Action: record.Bump, Destination: record.Published, Targets: []record.Target{{Name: "a"}}}},
		Attempts: []record.Attempt{{State: record.AttemptQueued, Spec: record.BuildSpec{Config: record.BuildConfig{Platform: platform}}}}}
	failed := JobStatus{Job: record.Job{ID: "job_f", ChangeID: "c2", State: record.JobFailed, Phase: record.PhaseVerification, Spec: record.JobSpec{Action: record.BumpRevision, Targets: []record.Target{{Name: "b"}}}},
		Attempts: []record.Attempt{{State: record.AttemptFinished, Evidence: &record.Evidence{Verdict: record.VerdictFailed, Failure: &record.Failure{Phase: "build"}}}}}
	attention := JobStatus{Job: record.Job{ID: "job_a", ChangeID: "c3", State: record.JobNeedsAttention, Phase: record.PhasePreparation, Spec: record.JobSpec{Action: record.Bump, Targets: []record.Target{{Name: "c"}}},
		Prepared: &record.PreparedChange{Branch: "b", PatchProblems: []string{"p1: rejects 4 hunks", "p2: rejects 1 hunk"}}}}
	ready := JobStatus{Job: record.Job{ID: "job_r", ChangeID: "c4", State: record.JobCompleted, Phase: record.PhasePreparation, ResultRevision: "r", Spec: record.JobSpec{Action: record.Bump, Destination: record.BranchReady, Targets: []record.Target{{Name: "d"}}}}}
	stopped := JobStatus{Job: record.Job{ID: "job_s", ChangeID: "c5", State: record.JobNeedsAttention, Phase: record.PhasePreparation, Detail: "forge: authentication is required", Spec: record.JobSpec{Action: record.Bump, Targets: []record.Target{{Name: "e"}}}}}
	rows := Project(Snapshot{Jobs: []JobStatus{waiting, failed, attention, ready, stopped}})
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
	require.Equal(t, "fix it, then dockhand bump e again, or abandon: forge: authentication is required", byPort["e"].Next, "a preparation that stopped before a branch is retried by bumping again")
}

func TestProjectFoldsAPortsContributionsUnderItsOpenOne(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	status := Snapshot{
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
	require.Equal(t, "stopped before a branch; fix it, then dockhand bump wasmer again: forge: authentication is required", row.Earlier[2].Next)
	require.True(t, row.Earlier[2].Retired)
}

func TestFinishedStandaloneVerificationsRetire(t *testing.T) {
	t.Parallel()
	verified := JobStatus{Job: record.Job{ID: "j", State: record.JobCompleted, Phase: record.PhaseVerification, Spec: record.JobSpec{Action: record.Verify, Destination: record.VerificationComplete, Targets: []record.Target{{Name: "libmd"}}}},
		Attempts: []record.Attempt{{State: record.AttemptFinished, Evidence: &record.Evidence{Verdict: record.VerdictPassed}}}}
	rows := Project(Snapshot{Jobs: []JobStatus{verified}})
	require.Len(t, rows, 1)
	require.Equal(t, "verified", rows[0].State)
	require.True(t, rows[0].Retired, "there is nothing to abandon or continue; it hides with the retired rows")
	require.Empty(t, Current(rows))
}

// A merged contribution's next column is its housekeeping: cleaned once
// both branches are settled, what is still owed or was kept otherwise, and
// only merged when it retired before cleanup was recorded.
func TestMergedNextWordsTheRecordedCleanup(t *testing.T) {
	t.Parallel()
	require.Equal(t, "merged", mergedNext(nil))
	both := &record.BranchCleanup{Local: record.CleanupOutcome{Name: "b", State: record.CleanupComplete}, Fork: record.CleanupOutcome{Name: "o/r:b", State: record.CleanupComplete}}
	require.Equal(t, "merged; branches cleaned", mergedNext(both))
	pending := &record.BranchCleanup{Local: record.CleanupOutcome{Name: "b", State: record.CleanupPending}, Fork: record.CleanupOutcome{Name: "o/r:b", State: record.CleanupPending, Detail: "kept: push failed"}}
	require.Equal(t, "merged; local branch b cleanup pending; fork branch o/r:b cleanup pending: kept: push failed", mergedNext(pending))
	kept := &record.BranchCleanup{Local: record.CleanupOutcome{Name: "b", State: record.CleanupKept, Detail: "kept; it no longer holds the published commit"}, Fork: record.CleanupOutcome{Name: "o/r:b", State: record.CleanupComplete}}
	require.Equal(t, "merged; local branch b kept; it no longer holds the published commit", mergedNext(kept))
}

func TestWordsForOneJobMatchItsRow(t *testing.T) {
	t.Parallel()
	job := record.Job{ID: "job_1", State: record.JobCompleted, Phase: record.PhaseVerification, ResultRevision: "revision",
		Spec:            record.JobSpec{Action: record.Bump, Destination: record.VerificationComplete, Targets: []record.Target{{Name: "jq", Variants: map[string]bool{"docs": true, "universal": false}}}},
		ResolvedRelease: &record.Release{Selection: record.Selection{CurrentVersion: "1.7"}, Version: "1.8.1"}}
	entry := JobStatus{Job: job, Attempts: []record.Attempt{{State: record.AttemptFinished, Evidence: &record.Evidence{Verdict: record.VerdictPassed}}}}
	require.Equal(t, "jq +docs -universal", PortLabel(job))
	require.Equal(t, "standalone job", PortLabel(record.Job{ID: "job_1"}), "a label never shows an identifier where a port name belongs")
	require.Equal(t, "jq", PortSelector(job))
	require.Equal(t, "--job job_1", PortSelector(record.Job{ID: "job_1"}), "a job without a target is selected by ID")
	require.Equal(t, "1.7 -> 1.8.1", ChangeWords(job))
	require.Equal(t, "verified", JobState(entry, nil))
	rows := Project(Snapshot{Jobs: []JobStatus{entry}})
	require.Equal(t, ChangeWords(job), rows[0].Change)
	require.Equal(t, JobState(entry, nil), rows[0].State)
	for action, words := range map[record.Action]string{record.Publish: "publication", record.Amend: "amendment", record.Rebase: "rebase", record.Verify: "verification", record.RefreshChecksums: "checksum refresh"} {
		require.Equal(t, words, ChangeWords(record.Job{Spec: record.JobSpec{Action: action}}))
	}
	current := &record.Release{Selection: record.Selection{CurrentVersion: "2.0", NoUpdate: true}, Version: "2.0"}
	require.Equal(t, "already current at 2.0", VersionMove(current))
	current.Version = "1.9"
	require.Equal(t, "already current at 2.0; latest eligible version is 1.9", VersionMove(current))
	require.Equal(t, "no update needed", JobState(JobStatus{Job: record.Job{State: record.JobCompleted, ResolvedRelease: current, Spec: record.JobSpec{Action: record.Bump}}}, nil))
}

// A verification that never judged the change, or that failed on something
// other than the target, says to run it again rather than to change anything.
func TestAFailureBeyondTheChangeAdvisesVerifyingAgain(t *testing.T) {
	t.Parallel()
	job := record.Job{ID: "job_1", ChangeID: "change_1", Phase: record.PhaseVerification, Detail: "verification errored",
		Spec: record.JobSpec{Action: record.Bump, Targets: []record.Target{{Name: "jq"}}}}
	change := record.Change{ID: "change_1", InitiatingTarget: "jq", Branch: "candidate", CurrentRevision: "revision", Disposition: record.ChangeOpen, Targets: []record.Target{{Name: "jq"}}}
	row := func(state record.JobState, evidence *record.Evidence) Contribution {
		entry := JobStatus{Job: job, Attempts: []record.Attempt{{ID: "attempt_1", State: record.AttemptFinished, Evidence: evidence}}}
		entry.Job.State = state
		rows := Project(Snapshot{Jobs: []JobStatus{entry}, Changes: []record.Change{change}})
		return rows[0]
	}

	// The machinery never reached a verdict.
	next := row(record.JobNeedsAttention, &record.Evidence{Verdict: record.VerdictErrored,
		Failure: &record.Failure{Kind: record.InfrastructureFailure, Detail: "guest agent never became ready"}}).Next
	require.Contains(t, next, "nothing to fix in the change, retry it: dockhand bump jq")
	require.Contains(t, next, "guest agent never became ready")
	require.NotContains(t, next, "amend")

	// A dependency failed, not the port being changed.
	next = row(record.JobFailed, &record.Evidence{Verdict: record.VerdictFailed,
		Failure: &record.Failure{Kind: record.DependencyFailure, Package: "openssl3", Phase: "build"}}).Next
	require.Contains(t, next, "dependency openssl3 failed to build, not the change itself")
	require.Contains(t, next, "retry it, or fix that port first: dockhand bump jq")

	// A dependency's failure names its phase and MacPorts' own reason.
	next = row(record.JobFailed, &record.Evidence{Verdict: record.VerdictFailed,
		Failure: &record.Failure{Kind: record.DependencyFailure, Package: "jxrlib", Phase: "fetch", Detail: "Failed to fetch jxrlib-1.4.3.tar.gz: The requested URL returned error: 404"}}).Next
	require.Equal(t, "dependency jxrlib failed to fetch, not the change itself (Failed to fetch jxrlib-1.4.3.tar.gz: The requested URL returned error: 404); retry it, or fix that port first: dockhand bump jq", next)

	// The port itself failed: that is the change's business, and amending is right.
	next = row(record.JobFailed, &record.Evidence{Verdict: record.VerdictFailed,
		Failure: &record.Failure{Kind: record.TargetFailure, Package: "jq", Phase: "build"}}).Next
	require.Equal(t, "build failed; fix and amend, or abandon", next)
	next = row(record.JobFailed, &record.Evidence{Verdict: record.VerdictFailed,
		Failure: &record.Failure{Kind: record.TargetFailure, Package: "jq", Phase: "fetch", Detail: "Failed to fetch jq-1.8.tar.gz: The requested URL returned error: 404\nFailed to fetch jq: Failed to fetch distfiles"}}).Next
	require.Equal(t, "fetch failed: Failed to fetch jq-1.8.tar.gz: The requested URL returned error: 404 Failed to fetch jq: Failed to fetch distfiles; fix and amend, or abandon", next)

	// An errored attempt with no failure record still says what it means.
	next = row(record.JobNeedsAttention, &record.Evidence{Verdict: record.VerdictErrored}).Next
	require.Contains(t, next, "errored before reaching a verdict")
	require.Contains(t, next, "dockhand bump jq", "the bump is what resumes its publication; verify would stop short")
}

// The verb that retries a row depends on how its contribution was made. One
// dockhand prepared is retried by its own preparing action, which adopts the
// branch already built; one adopted from someone's own branch was never
// prepared, so verifying it again is the retry.
func TestTheRetryVerbFollowsHowTheContributionWasMade(t *testing.T) {
	t.Parallel()
	row := func(action record.Action, state record.JobState) Contribution {
		job := record.Job{ID: "job_1", ChangeID: "change_1", State: state, Phase: record.PhaseVerification,
			Spec: record.JobSpec{Action: action, Targets: []record.Target{{Name: "jq"}}}}
		change := record.Change{ID: "change_1", InitiatingTarget: "jq", Branch: "candidate", CurrentRevision: "revision", Disposition: record.ChangeOpen, Targets: []record.Target{{Name: "jq"}}}
		rows := Project(Snapshot{Jobs: []JobStatus{{Job: job}}, Changes: []record.Change{change}})
		return rows[0]
	}
	for action, want := range map[record.Action]string{
		record.Bump:             "bump",
		record.BumpRevision:     "bump-revision",
		record.RefreshChecksums: "checksums",
		record.Publish:          "publish",
		// A branch dockhand did not prepare: its work is a verification.
		record.Verify: "verify",
		// Corrections recapture a checkout, so retrying one re-runs the build.
		record.Amend:  "verify",
		record.Rebase: "verify",
	} {
		require.Equal(t, want, row(action, record.JobNeedsAttention).Retry, "%s", action)
	}
	require.Empty(t, row(record.Bump, record.JobActive).Retry, "nothing stopped")
	require.Empty(t, row(record.Bump, record.JobCompleted).Retry, "nothing stopped")
	require.Equal(t, "bump", row(record.Bump, record.JobFailed).Retry)
}

// Every line that tells a person what to run names the contribution's own
// command. A bump is never advised to verify, which would re-run the build
// and stop short of the pull request it was going to open.
func TestStoppedWorkIsAlwaysAdvisedWithItsOwnCommand(t *testing.T) {
	t.Parallel()
	change := record.Change{ID: "change_1", InitiatingTarget: "jq", Branch: "candidate", CurrentRevision: "revision", Disposition: record.ChangeOpen, Targets: []record.Target{{Name: "jq"}}}
	next := func(action record.Action, state record.JobState, phase record.JobPhase, detail string) string {
		job := record.Job{ID: "job_1", ChangeID: "change_1", State: state, Phase: phase, Detail: detail,
			Spec: record.JobSpec{Action: action, Targets: []record.Target{{Name: "jq"}}}}
		if phase == record.PhaseVerification {
			job.Prepared = &record.PreparedChange{Branch: "candidate"}
		}
		return Project(Snapshot{Jobs: []JobStatus{{Job: job}}, Changes: []record.Change{change}})[0].Next
	}

	// An unsupported verdict: the person fixes the cause, and the command
	// they are given starts a new attempt rather than resuming a closed one.
	advice := next(record.BumpRevision, record.JobNeedsAttention, record.PhaseVerification, "contribution must contain one commit above its base")
	require.Contains(t, advice, "dockhand bump-revision jq again")
	require.Contains(t, advice, "starts a new attempt")
	require.Contains(t, advice, "one commit above its base")
	require.NotContains(t, advice, "verify again", "a revision bump is not retried by verifying")

	// Canceled work, and a preparation that failed before building anything.
	require.Contains(t, next(record.Bump, record.JobCanceled, record.PhaseVerification, ""), "dockhand bump jq again")
	require.Contains(t, next(record.RefreshChecksums, record.JobFailed, record.PhasePreparation, "evaluation failed"), "dockhand checksums jq again")

	// A branch dockhand never prepared has no action to re-run but its own.
	require.Contains(t, next(record.Verify, record.JobNeedsAttention, record.PhaseVerification, "workflow is disabled"), "dockhand verify jq again")
}
