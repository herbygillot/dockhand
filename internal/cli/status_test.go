package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestStatusSelectorsCLIReadRecordedBranchWithoutExecution(t *testing.T) {
	config, id := queuedJob(t)
	// The recorded association remains queryable after its Git ref is removed.
	cmd := exec.CommandContext(t.Context(), "git", "update-ref", "-d", "refs/heads/candidate")
	cmd.Dir = config.Repository
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	for _, args := range [][]string{
		{"status", string(id)}, {"status", "--active"}, {"status", "--branch", "candidate"},
		{"status", "--branch", "candidate", "--active"}, {"status", string(id), "--active"},
	} {
		var stdout, stderr bytes.Buffer
		require.NoError(t, Run(t.Context(), append(args, "--json"), Streams{Out: &stdout, Err: &stderr}, config))
		var result workflow.Status
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
		require.Len(t, result.Jobs, 1)
		require.Equal(t, id, result.Jobs[0].Job.ID)
		require.Equal(t, record.JobQueued, result.Jobs[0].Job.State)
		require.Equal(t, record.PhaseVerification, result.Jobs[0].Job.Phase)
		require.Empty(t, result.Jobs[0].Attempts)
		require.Len(t, result.Changes, 1)
		require.Empty(t, stderr.String())
	}
	var outbuf bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"status", string(id)}, Streams{Out: &outbuf, Err: &outbuf}, config))
	require.Contains(t, outbuf.String(), "phase: verification")
	outbuf.Reset()
	require.NoError(t, Run(t.Context(), []string{"status", "--branch", "missing", "--active"}, Streams{Out: &outbuf, Err: &outbuf}, config))
	require.Contains(t, outbuf.String(), "Contribution branch: missing")
	require.Contains(t, outbuf.String(), "Showing queued and active jobs.")
	require.Contains(t, outbuf.String(), "No matching jobs.")
	require.NotContains(t, outbuf.String(), string(id))
	require.ErrorIs(t, Run(t.Context(), []string{"status", "unknown"}, Streams{Out: &outbuf, Err: &outbuf}, config), state.ErrNotFound)
}

func TestStatusRejectsInvalidSelectorsBeforeOpeningRepository(t *testing.T) {
	config := app.Config{Repository: "/missing/repository", DBPath: filepath.Join(t.TempDir(), "absent", "state.db")}
	for _, args := range [][]string{
		{"status", "job", "--branch", "candidate"}, {"status", "job", "second"},
		{"status", ""}, {"status", "bad\njob"}, {"status", "--branch="}, {"status", "--branch", "bad..branch"},
	} {
		var out bytes.Buffer
		err := Run(t.Context(), args, Streams{Out: &out, Err: &out}, config)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "git ")
	}
	_, err := os.Stat(filepath.Dir(config.DBPath))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestStatusRendersWorkingTreeFileCount(t *testing.T) {
	status := workflow.EmptyStatus(time.Now())
	status.Jobs = []workflow.JobStatus{{Job: record.Job{
		ID:    "job_fixture",
		State: record.JobCompleted,
		Spec: record.JobSpec{
			Action:       record.Verify,
			Destination:  record.VerificationComplete,
			Verification: record.VerificationRequired,
			Source:       record.Source{Tree: "fixture"},
			Checkout:     &record.Checkout{Branch: "main", Head: "fixture", ModifiedFiles: 2},
		},
	}, Attempts: []record.Attempt{{
		ID: "attempt", State: record.AttemptFinished,
		Spec: record.BuildSpec{Target: record.Target{Name: "fixture"}, Config: record.BuildConfig{Platform: record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}},
		Evidence: &record.Evidence{
			Verdict: record.VerdictPassed, ObservedAt: time.Now(), Environment: &record.EnvironmentEvidence{
				Provider: "tart", EnvironmentDigest: "sha256:image", CapabilityDigest: "sha256:capabilities",
				Capabilities: record.EnvironmentCapabilities{MacPortsPrefix: "/opt/local", MacPortsVersion: "2.12.6", DeveloperTools: record.DeveloperToolsXcode, XcodeVersion: "26.6"},
			},
		},
	}}}}
	var output bytes.Buffer
	require.NoError(t, renderStatus(&output, status))
	require.Contains(t, output.String(), "2 modified files")
	require.Contains(t, output.String(), "environment: tart sha256:image; capabilities: sha256:capabilities")
	require.Contains(t, output.String(), "MacPorts: 2.12.6 at /opt/local; developer tools: xcode 26.6")
	require.NotContains(t, output.String(), "%!")
}

func TestStatusRendersGitHubRunIdentity(t *testing.T) {
	status := workflow.EmptyStatus(time.Now())
	status.Jobs = []workflow.JobStatus{{Job: record.Job{ID: "job", State: record.JobCompleted}, Attempts: []record.Attempt{{
		ID: "attempt", State: record.AttemptFinished,
		Evidence: &record.Evidence{Verdict: record.VerdictPassed, Workflow: &record.WorkflowEvidence{RunID: 34989358751, RunAttempt: 2, Conclusion: "success", URL: "https://github.com/owner/ports/actions/runs/34989358751"}},
	}}}}
	var output bytes.Buffer
	require.NoError(t, renderStatus(&output, status))
	require.Contains(t, output.String(), "run 34989358751 attempt 2")
	require.NotContains(t, output.String(), "%!")
}

func TestStatusShowsQueuedGitHubRunAndForkBranch(t *testing.T) {
	status := workflow.EmptyStatus(time.Now())
	status.Jobs = []workflow.JobStatus{{Job: record.Job{ID: "job", State: record.JobActive}, Attempts: []record.Attempt{{
		State: record.AttemptRunning, Evidence: &record.Evidence{Workflow: &record.WorkflowEvidence{Repository: "owner/ports", Branch: "update", Commit: "abc", RunID: 10, RunAttempt: 2, Status: "queued", URL: "https://github.com/owner/ports/actions/runs/10"}},
	}}}}
	var output bytes.Buffer
	require.NoError(t, renderStatus(&output, status))
	require.Contains(t, output.String(), "GitHub Actions: queued; run 10 attempt 2")
	require.Contains(t, output.String(), "Fork branch: owner/ports:update at abc")
}

func TestStatusDistinguishesFailedBumpFromStandaloneVerification(t *testing.T) {
	target := record.Target{Name: "terraform-1.16", Subport: "terraform-1.16", Portfile: "sysutils/terraform/Portfile"}
	status := workflow.EmptyStatus(time.Now())
	status.Jobs = []workflow.JobStatus{
		{Job: record.Job{ID: "bump", State: record.JobNeedsAttention, Phase: record.PhasePreparation,
			Spec: record.JobSpec{Action: record.Bump, Targets: []record.Target{target}}}},
		{Job: record.Job{ID: "verify", State: record.JobCompleted, Phase: record.PhaseVerification,
			Spec: record.JobSpec{Action: record.Verify, Targets: []record.Target{target},
				EvaluatedVersions: map[string]string{target.Name: "1.16.0"},
				Checkout:          &record.Checkout{Branch: "master", ModifiedFiles: 0}}},
			Attempts: []record.Attempt{{State: record.AttemptFinished, Evidence: &record.Evidence{Verdict: record.VerdictPassed}}}},
	}
	var output bytes.Buffer
	require.NoError(t, renderStatus(&output, status))
	require.Contains(t, output.String(), "preparation stopped; no update branch was created")
	require.Contains(t, output.String(), "verification passed for standalone source; no update was prepared")
	require.Contains(t, output.String(), "input version: 1.16.0")
	require.Contains(t, output.String(), "working tree (master)")
	require.NotContains(t, output.String(), "terraform-1.16/terraform-1.16")
	require.NotContains(t, output.String(), "prepared branch:")
}

func TestStatusDoesNotDescribeUnintegratedCandidateAsBranch(t *testing.T) {
	status := workflow.EmptyStatus(time.Now())
	job := record.Job{ID: "bump", Phase: record.PhasePreparation, State: record.JobNeedsAttention,
		Prepared: &record.PreparedChange{Branch: "candidate"}}
	status.Jobs = []workflow.JobStatus{{Job: job}}
	var output bytes.Buffer
	require.NoError(t, renderStatus(&output, status))
	require.Contains(t, output.String(), "candidate awaiting confirmed branch integration: candidate")
	require.NotContains(t, output.String(), "prepared branch:")
	status.Jobs[0].Job.ResultRevision = "revision"
	output.Reset()
	require.NoError(t, renderStatus(&output, status))
	require.Contains(t, output.String(), "prepared branch: candidate")
}
