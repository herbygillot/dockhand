package github

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"strings"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

func matches(saved payload, run *gh.WorkflowRun) bool {
	return run != nil && run.GetHeadSHA() == string(saved.Request.Spec.Source.Commit) && run.GetHeadBranch() == saved.Request.Spec.PushBranch() && run.GetEvent() == "push" && run.GetWorkflowID() == saved.Config.WorkflowID && run.GetPath() == macports.PortsWorkflowPath && strings.EqualFold(run.GetRepository().GetFullName(), saved.Config.Destination.HeadRepository) && strings.EqualFold(run.GetHeadRepository().GetFullName(), saved.Config.Destination.HeadRepository)
}

func (p *Provider) execution(ctx context.Context, handle record.ProviderRun) (payload, executionRun, record.ProviderExecution, error) {
	var saved payload
	var run executionRun
	var row record.ProviderExecution
	if handle.Provider != verify.ProviderGitHub || handle.RequestID == "" {
		return saved, run, row, fmt.Errorf("github verification: invalid run handle")
	}
	row, err := p.read(ctx, handle.RequestID)
	if err != nil {
		return saved, run, row, err
	}
	if row.State != record.ExecutionAdmitted && row.State != record.ExecutionReleased {
		return saved, run, row, fmt.Errorf("github verification: run has not been admitted")
	}
	if err = json.Unmarshal(row.Payload, &saved); err != nil {
		return saved, run, row, err
	}
	if err = json.Unmarshal(row.Result, &run); err != nil {
		return saved, run, row, err
	}
	if handle.RunID != fmt.Sprintf("%d:%d", run.ID, run.Attempt) {
		return saved, run, row, fmt.Errorf("github verification: run handle does not match the stored attempt")
	}
	return saved, run, row, nil
}

func (p *Provider) Observe(ctx context.Context, handle record.ProviderRun) (verify.Observation, error) {
	result := verify.Observation{Run: handle, State: record.AttemptRunning, Verdict: record.VerdictUnknown, ObservedAt: time.Now().UTC()}
	saved, selected, row, err := p.execution(ctx, handle)
	if err != nil {
		return result, err
	}
	if row.State == record.ExecutionReleased {
		result.State, result.Verdict = record.AttemptCanceled, record.VerdictCanceled
		result.Detail = "Stopped tracking GitHub Actions run; the remote run was not canceled"
		return result, nil
	}
	api, err := p.actions(ctx, saved.Config.Destination.HeadRepository)
	if err != nil {
		return result, err
	}
	run, err := api.Run(ctx, selected.ID, selected.Attempt)
	if err != nil {
		return result, err
	}
	if !matches(saved, run) || run.GetID() != selected.ID || run.GetRunAttempt() != selected.Attempt {
		return result, fmt.Errorf("github verification: workflow run identity changed")
	}
	jobs, err := api.Jobs(ctx, selected.ID, selected.Attempt)
	if err != nil {
		return result, err
	}
	evidence := &record.WorkflowEvidence{Repository: saved.Config.Destination.HeadRepository, Branch: saved.Request.Spec.PushBranch(), Commit: saved.Request.Spec.Source.Commit, Path: macports.PortsWorkflowPath, RunID: selected.ID, RunAttempt: selected.Attempt, URL: run.GetHTMLURL(), Status: run.GetStatus(), Conclusion: run.GetConclusion()}
	seen := map[string]bool{}
	// A run establishes a macOS build when it carries jobs, each one ran to a
	// successful conclusion on a macOS runner, and no name appears twice. The
	// workflow's own prediction, when it made a sound one, is held to as well.
	complete, incomplete := len(jobs) > 0, "the workflow ran no jobs"
	fail := func(reason string) {
		if complete {
			complete, incomplete = false, reason
		}
	}
	for _, job := range jobs {
		if job.GetRunID() != selected.ID || job.GetRunAttempt() != int64(selected.Attempt) || job.GetHeadSHA() != string(saved.Request.Spec.Source.Commit) {
			return result, fmt.Errorf("github verification: job identifies a different run attempt or commit")
		}
		if seen[job.GetName()] {
			fail("job " + job.GetName() + " appears twice")
		}
		seen[job.GetName()] = true
		if job.GetStatus() != "completed" || job.GetConclusion() != "success" {
			fail("job " + job.GetName() + " is " + job.GetStatus() + " " + job.GetConclusion())
		}
		// Labels are the job's own runs-on values. An older run may not carry
		// them, and an absent label says nothing either way.
		if len(job.Labels) > 0 && !macOSRunner(job.Labels) {
			fail("job " + job.GetName() + " did not run on macOS")
		}
		result.Logs = append(result.Logs, record.Artifact{Name: job.GetName() + " logs", Location: job.GetHTMLURL(), MediaType: "text/html"})
		evidence.Jobs = append(evidence.Jobs, record.WorkflowJob{ID: job.GetID(), Name: job.GetName(), URL: job.GetHTMLURL(), Status: job.GetStatus(), Conclusion: job.GetConclusion(), Labels: job.Labels})
	}
	if len(saved.Matrix) > 0 {
		if len(jobs) != len(saved.Matrix) {
			fail(fmt.Sprintf("the workflow declared %d jobs and the run carried %d", len(saved.Matrix), len(jobs)))
		}
		for _, name := range saved.Matrix {
			if !seen[name] {
				fail("the workflow declared job " + name + ", which the run did not carry")
			}
		}
	}
	result.Workflow = evidence
	result.TestOmission = "GitHub workflow policy permits port test failures; individual test success is not established"
	selected.URL = run.GetHTMLURL()
	result.Detail = runDetail(saved, selected, run.GetStatus())
	if run.GetStatus() != "completed" {
		return result, nil
	}
	result.State = record.AttemptFinished
	switch run.GetConclusion() {
	case "success":
		result.Verdict = record.VerdictPassed
		if !complete {
			result.Verdict = record.VerdictBlocked
			result.Detail = "GitHub workflow succeeded but its jobs do not establish a complete macOS build: " + incomplete
		}
	case "failure":
		result.Verdict = record.VerdictFailed
	case "cancelled":
		result.State = record.AttemptCanceled
		result.Verdict = record.VerdictCanceled
	case "timed_out", "startup_failure":
		result.Verdict = record.VerdictErrored
	default:
		result.Verdict = record.VerdictBlocked
	}
	result.Detail += "; workflow conclusion: " + run.GetConclusion()
	return result, nil
}

// Cancel releases this request's tracking, not the shared remote workflow run.
// A push-triggered run has no exclusive owner or attempt-specific cancellation API.
func (p *Provider) Cancel(ctx context.Context, handle record.ProviderRun) error {
	return p.locked(ctx, handle.RequestID, func(ctx context.Context) error {
		_, _, row, err := p.execution(ctx, handle)
		if err != nil {
			return err
		}
		if row.State == record.ExecutionReleased {
			return nil
		}
		row.State, row.Occupied = record.ExecutionReleased, false
		return p.put(ctx, row)
	})
}

func (p *Provider) Release(context.Context, record.ResourceHandle) (verify.ReleaseResult, error) {
	return verify.ReleaseResult{}, fmt.Errorf("github verification owns no releasable VM resources")
}

var _ verify.Provider = (*Provider)(nil)

// macOSRunner reports whether a job's runs-on labels name a macOS runner,
// covering both the hosted "macos-15" spelling and a "macOS" self-hosted label.
func macOSRunner(labels []string) bool {
	for _, label := range labels {
		if strings.HasPrefix(strings.ToLower(label), "macos") {
			return true
		}
	}
	return false
}
