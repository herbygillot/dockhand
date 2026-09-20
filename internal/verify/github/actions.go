package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v91/github"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// newActions binds the shared authenticated SDK to one repository.
func newActions(ctx context.Context, c *githubapi.Client, repository string) (*actionsClient, error) {
	if !githubapi.ValidRepositoryName(repository) {
		return nil, fmt.Errorf("github: invalid Actions repository")
	}
	api, err := c.AuthenticatedAPI(ctx)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	owner, name, _ := strings.Cut(repository, "/")
	return &actionsClient{service: api.Actions, owner: owner, repository: name}, nil
}

type actionsClient struct {
	service           *gh.ActionsService
	owner, repository string
}

func (a *actionsClient) Workflow(ctx context.Context, filename string) (*gh.Workflow, error) {
	value, _, err := a.service.GetWorkflowByFileName(ctx, a.owner, a.repository, filename)
	return value, githubapi.RateLimitError(err)
}

func (a *actionsClient) Runs(ctx context.Context, workflow int64, branch, commit string) ([]*gh.WorkflowRun, error) {
	options := &gh.ListWorkflowRunsOptions{Branch: branch, HeadSHA: commit, Event: "push"}
	var runs []*gh.WorkflowRun
	for {
		page, response, err := a.service.ListWorkflowRunsByID(ctx, a.owner, a.repository, workflow, options)
		if err != nil {
			return nil, githubapi.RateLimitError(err)
		}
		runs = append(runs, page.WorkflowRuns...)
		if response.NextPage == 0 {
			return runs, nil
		}
		options.Page = response.NextPage
	}
}

func (a *actionsClient) Run(ctx context.Context, id int64, attempt int) (*gh.WorkflowRun, error) {
	if attempt == 0 {
		value, _, err := a.service.GetWorkflowRunByID(ctx, a.owner, a.repository, id)
		return value, githubapi.RateLimitError(err)
	}
	value, _, err := a.service.GetWorkflowRunAttempt(ctx, a.owner, a.repository, id, attempt, nil)
	return value, githubapi.RateLimitError(err)
}

// Rerun asks GitHub to run this run's unsuccessful jobs again, which adds an
// attempt to the same run rather than creating another one. Only the legs that
// did not succeed are repeated, so retrying a matrix costs one runner rather
// than all of them. The call returns no identity, so the caller re-reads the
// run to learn the attempt it produced.
func (a *actionsClient) Rerun(ctx context.Context, id int64) error {
	_, err := a.service.RerunFailedJobsByID(ctx, a.owner, a.repository, id)
	return githubapi.RateLimitError(err)
}

func (a *actionsClient) Jobs(ctx context.Context, id int64, attempt int) ([]*gh.WorkflowJob, error) {
	options := &gh.ListOptions{}
	var jobs []*gh.WorkflowJob
	for {
		page, response, err := a.service.ListWorkflowJobsAttempt(ctx, a.owner, a.repository, id, int64(attempt), options)
		if err != nil {
			return nil, githubapi.RateLimitError(err)
		}
		jobs = append(jobs, page.Jobs...)
		if response.NextPage == 0 {
			return jobs, nil
		}
		options.Page = response.NextPage
	}
}

// JobLog follows the SDK-provided download URL without forwarding API credentials.
func (a *actionsClient) JobLog(ctx context.Context, id int64) (io.ReadCloser, error) {
	location, _, err := a.service.GetWorkflowJobLogs(ctx, a.owner, a.repository, id, 0)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	if location == nil || location.Scheme != "https" || location.Host == "" || location.User != nil {
		return nil, fmt.Errorf("github: invalid job log download URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("github: job logs returned %s", response.Status)
	}
	return response.Body, nil
}

func (p *Provider) actions(ctx context.Context, repository string) (actionsAPI, error) {
	if p.backend != nil {
		return p.backend(ctx, repository)
	}
	return newActions(ctx, p.Client, repository)
}
