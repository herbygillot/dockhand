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
	complete := len(jobs) == len(saved.Matrix)
	for _, job := range jobs {
		if job.GetRunID() != selected.ID || job.GetRunAttempt() != int64(selected.Attempt) || job.GetHeadSHA() != string(saved.Request.Spec.Source.Commit) {
			return result, fmt.Errorf("github verification: job identifies a different run attempt or commit")
		}
		if seen[job.GetName()] {
			complete = false
		}
		seen[job.GetName()] = true
		if job.GetStatus() != "completed" || job.GetConclusion() != "success" {
			complete = false
		}
		result.Logs = append(result.Logs, record.Artifact{Name: job.GetName() + " logs", Location: job.GetHTMLURL(), MediaType: "text/html"})
		evidence.Jobs = append(evidence.Jobs, record.WorkflowJob{ID: job.GetID(), Name: job.GetName(), URL: job.GetHTMLURL(), Status: job.GetStatus(), Conclusion: job.GetConclusion(), Labels: job.Labels})
	}
	for _, name := range saved.Matrix {
		if !seen[name] {
			complete = false
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
			result.Detail = "GitHub workflow succeeded without a complete successful build matrix"
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
