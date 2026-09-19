package cli

import (
	"bytes"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestSummaryNamesPortVersionBranchVerdictAndPR(t *testing.T) {
	t.Parallel()
	platform := record.Platform{OS: "macOS", Version: "26", Architecture: "arm64"}
	status := workflow.EmptyStatus(time.Now())
	status.Jobs = []view.JobStatus{{
		Job: record.Job{ID: "job_1", ChangeID: "change_1", State: record.JobCompleted, Phase: record.PhasePublication, ResultRevision: "revision",
			Spec:            record.JobSpec{Action: record.Bump, Destination: record.Published, Targets: []record.Target{{Name: "jq", Portfile: "sysutils/jq/Portfile", Variants: map[string]bool{"docs": true}}}},
			ResolvedRelease: &record.Release{Selection: record.Selection{CurrentVersion: "1.7"}, Version: "1.8.1", Tag: "jq-1.8.1"},
			Prepared:        &record.PreparedChange{Branch: "dockhand/bump/jq-1.8.1"}},
		Attempts:     []record.Attempt{{ID: "attempt_1", State: record.AttemptFinished, Spec: record.BuildSpec{Target: record.Target{Name: "jq"}, Config: record.BuildConfig{Platform: platform}}, Evidence: &record.Evidence{Verdict: record.VerdictPassed}}},
		Publications: []record.PublicationAction{{ID: "publication_1", State: record.PublicationConfirmed}},
	}}
	status.PullRequests = []record.PullRequest{{ChangeID: "change_1", State: record.PullRequestOpen, Ref: record.PullRequestRef{URL: "https://github.com/macports/macports-ports/pull/1"}}}
	var out bytes.Buffer
	require.NoError(t, renderSummary(&out, status))
	require.Equal(t, "jq +docs: 1.7 -> 1.8.1; published\n  branch: dockhand/bump/jq-1.8.1\n  passed on macOS 26 arm64\n  PR https://github.com/macports/macports-ports/pull/1 (created)\n", out.String())
	require.NotContains(t, out.String(), "job_1")
	require.NotContains(t, out.String(), "change_1")
}

func TestSummaryShowsFailingPhaseLogAndResumeByPort(t *testing.T) {
	t.Parallel()
	platform := record.Platform{OS: "macOS", Version: "15", Architecture: "arm64"}
	failed := view.JobStatus{
		Job: record.Job{ID: "job_2", State: record.JobFailed, Phase: record.PhaseVerification, Detail: "verification failed",
			Spec:     record.JobSpec{Action: record.BumpRevision, Destination: record.VerificationComplete, Targets: []record.Target{{Name: "deno"}}},
			Prepared: &record.PreparedChange{Branch: "dockhand/revbump/deno"}, ResultRevision: "revision"},
		Attempts: []record.Attempt{{State: record.AttemptFinished, Spec: record.BuildSpec{Target: record.Target{Name: "deno"}, Config: record.BuildConfig{Platform: platform}},
			Evidence: &record.Evidence{Verdict: record.VerdictFailed, Failure: &record.Failure{Phase: "build", Package: "deno", Detail: "error: linking failed"}, Logs: []record.Artifact{{Name: "build.log", Location: "/tmp/attempt/build.log"}}}}},
	}
	pending := view.JobStatus{Job: record.Job{ID: "job_3", State: record.JobActive, Phase: record.PhaseVerification,
		Spec: record.JobSpec{Action: record.Verify, Destination: record.VerificationComplete, Targets: []record.Target{{Name: "jq"}}}},
		Attempts: []record.Attempt{{State: record.AttemptQueued, Spec: record.BuildSpec{Target: record.Target{Name: "jq"}, Config: record.BuildConfig{Platform: platform}}}}}
	var out bytes.Buffer
	require.NoError(t, renderSummary(&out, workflow.Status{Jobs: []view.JobStatus{failed, pending}}))
	text := out.String()
	require.Contains(t, text, "deno: revision bump; failed\n  branch: dockhand/revbump/deno\n  failed on macOS 15 arm64; build phase: error: linking failed; log: /tmp/attempt/build.log\n  verification failed\n")
	require.Contains(t, text, "jq: verification; waiting for capacity\n  waiting for a build slot on macOS 15 arm64\n")
	require.Contains(t, text, "Resume with dockhand wait jq or run dockhand start")
	require.NotContains(t, text, "job_")
}

func TestSummaryReportsCurrentPortsAndReusedEvidence(t *testing.T) {
	t.Parallel()
	platform := record.Platform{OS: "macOS", Version: "26", Architecture: "arm64"}
	status := workflow.Status{Jobs: []view.JobStatus{
		{Job: record.Job{State: record.JobCompleted, Spec: record.JobSpec{Action: record.Bump, Destination: record.VerificationComplete, Targets: []record.Target{{Name: "gh"}}}, ResolvedRelease: &record.Release{Selection: record.Selection{CurrentVersion: "2.0", NoUpdate: true}, Version: "2.0"}}},
		{Job: record.Job{State: record.JobCompleted, ReusedAttempt: "attempt_0", Spec: record.JobSpec{Action: record.Verify, Destination: record.VerificationComplete, Targets: []record.Target{{Name: "jq"}}}},
			Reused: &record.Attempt{Spec: record.BuildSpec{Config: record.BuildConfig{Platform: platform}}, Evidence: &record.Evidence{Verdict: record.VerdictPassed}}},
	}}
	var out bytes.Buffer
	require.NoError(t, renderSummary(&out, status))
	require.Equal(t, "gh: already current at 2.0; no update needed\n\njq: verification; verified\n  passed on macOS 26 arm64 (reused from an earlier build)\n", out.String())
}

// The summary and the status table read a job through the same words.
func TestSummaryAndProjectionAgreeOnAJobsWords(t *testing.T) {
	t.Parallel()
	platform := record.Platform{OS: "macOS", Version: "26", Architecture: "arm64"}
	entry := view.JobStatus{
		Job: record.Job{ID: "job_9", ChangeID: "change_9", State: record.JobActive, Phase: record.PhasePreparation,
			Spec:            record.JobSpec{Action: record.Bump, Destination: record.Published, Verification: record.VerificationRequired, Targets: []record.Target{{Name: "jq", Portfile: "sysutils/jq/Portfile"}}},
			ResolvedRelease: &record.Release{Selection: record.Selection{CurrentVersion: "1.7"}, Version: "1.8.1"},
			Prepared:        &record.PreparedChange{Branch: "dockhand/bump/jq-1.8.1"}},
		Attempts: []record.Attempt{{State: record.AttemptQueued, Spec: record.BuildSpec{Target: record.Target{Name: "jq"}, Config: record.BuildConfig{Platform: platform}}}},
	}
	status := workflow.Status{Snapshot: view.Snapshot{Jobs: []view.JobStatus{entry}, Changes: []record.Change{{ID: "change_9", InitiatingTarget: "jq", Disposition: record.ChangeOpen, Targets: []record.Target{{Name: "jq"}}}}}}
	rows := view.Project(status.Snapshot)
	require.Len(t, rows, 1)
	var out bytes.Buffer
	require.NoError(t, renderSummary(&out, status))
	require.Contains(t, out.String(), "jq: "+rows[0].Change+"; "+rows[0].State+"\n")
	require.Equal(t, "integrating branch", rows[0].State)
}
