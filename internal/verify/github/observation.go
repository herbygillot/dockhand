package github

import (
	"context"
	"encoding/json"
	"fmt"
	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
	"strings"
	"time"
)

func matches(saved payload, run *gh.WorkflowRun) bool {
	return run != nil && run.GetHeadSHA() == string(saved.Request.Spec.Source.Commit) && run.GetHeadBranch() == saved.Request.Spec.Branch && run.GetEvent() == "push" && run.GetWorkflowID() == saved.Config.WorkflowID && run.GetPath() == WorkflowPath && strings.EqualFold(run.GetRepository().GetFullName(), saved.Config.Destination.HeadRepository) && strings.EqualFold(run.GetHeadRepository().GetFullName(), saved.Config.Destination.HeadRepository)
}

func (p *Provider) execution(ctx context.Context, handle record.ProviderRun) (payload, executionRun, Actions, error) {
	var saved payload
	var run executionRun
	if handle.Provider != ProviderName || handle.RequestID == "" {
		return saved, run, nil, fmt.Errorf("github verification: invalid run handle")
	}
	row, err := p.read(ctx, handle.RequestID)
	if err != nil {
		return saved, run, nil, err
	}
	if row.State != record.ExecutionAdmitted {
		return saved, run, nil, fmt.Errorf("github verification: run has not been admitted")
	}
	if err = json.Unmarshal(row.Payload, &saved); err != nil {
		return saved, run, nil, err
	}
	if err = json.Unmarshal(row.Result, &run); err != nil {
		return saved, run, nil, err
	}
	if handle.RunID != fmt.Sprintf("%d:%d", run.ID, run.Attempt) {
		return saved, run, nil, fmt.Errorf("github verification: run handle does not match the stored attempt")
	}
	api, err := p.Actions(ctx, saved.Config.Destination.HeadRepository)
	return saved, run, api, err
}

func (p *Provider) Observe(ctx context.Context, handle record.ProviderRun) (verify.Observation, error) {
	result := verify.Observation{Run: handle, State: record.AttemptRunning, Verdict: record.VerdictUnknown, ObservedAt: time.Now().UTC()}
	saved, selected, api, err := p.execution(ctx, handle)
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
	evidence := &record.WorkflowEvidence{Repository: saved.Config.Destination.HeadRepository, Branch: saved.Request.Spec.Branch, Commit: saved.Request.Spec.Source.Commit, Path: WorkflowPath, RunID: selected.ID, RunAttempt: selected.Attempt, URL: run.GetHTMLURL(), Conclusion: run.GetConclusion()}
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
	result.Detail = "GitHub Actions: " + run.GetStatus() + "; " + run.GetHTMLURL()
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

func (p *Provider) Cancel(ctx context.Context, handle record.ProviderRun) error {
	saved, selected, api, err := p.execution(ctx, handle)
	if err != nil {
		return err
	}
	current, err := api.Run(ctx, selected.ID, 0)
	if err != nil {
		return err
	}
	if !matches(saved, current) {
		return fmt.Errorf("github verification: cancellation run identity changed")
	}
	if current.GetStatus() == "completed" || current.GetRunAttempt() != selected.Attempt {
		return nil
	}
	return api.Cancel(ctx, selected.ID)
}

func (p *Provider) Release(context.Context, record.ResourceHandle) (verify.ReleaseResult, error) {
	return verify.ReleaseResult{}, fmt.Errorf("github verification owns no releasable VM resources")
}

var _ verify.Provider = (*Provider)(nil)
