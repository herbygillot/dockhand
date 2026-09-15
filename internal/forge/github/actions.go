package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v91/github"
)

// Actions returns an authenticated SDK service bound to one repository.
func (c *Client) Actions(ctx context.Context, repository string) (*Actions, error) {
	if !validRepositoryName(repository) {
		return nil, fmt.Errorf("github: invalid Actions repository")
	}
	api, err := c.authenticatedAPI(ctx)
	if err != nil {
		return nil, err
	}
	owner, name, _ := strings.Cut(repository, "/")
	return &Actions{service: api.Actions, owner: owner, repository: name}, nil
}

type Actions struct {
	service           *gh.ActionsService
	owner, repository string
}

func (a *Actions) Workflow(ctx context.Context, filename string) (*gh.Workflow, error) {
	value, _, err := a.service.GetWorkflowByFileName(ctx, a.owner, a.repository, filename)
	return value, err
}

func (a *Actions) Runs(ctx context.Context, workflow int64, branch, commit string) ([]*gh.WorkflowRun, error) {
	options := &gh.ListWorkflowRunsOptions{Branch: branch, HeadSHA: commit, Event: "push"}
	var runs []*gh.WorkflowRun
	for {
		page, response, err := a.service.ListWorkflowRunsByID(ctx, a.owner, a.repository, workflow, options)
		if err != nil {
			return nil, err
		}
		runs = append(runs, page.WorkflowRuns...)
		if response.NextPage == 0 {
			return runs, nil
		}
		options.Page = response.NextPage
	}
}

func (a *Actions) Run(ctx context.Context, id int64, attempt int) (*gh.WorkflowRun, error) {
	if attempt == 0 {
		value, _, err := a.service.GetWorkflowRunByID(ctx, a.owner, a.repository, id)
		return value, err
	}
	value, _, err := a.service.GetWorkflowRunAttempt(ctx, a.owner, a.repository, id, attempt, nil)
	return value, err
}

func (a *Actions) Jobs(ctx context.Context, id int64, attempt int) ([]*gh.WorkflowJob, error) {
	options := &gh.ListOptions{}
	var jobs []*gh.WorkflowJob
	for {
		page, response, err := a.service.ListWorkflowJobsAttempt(ctx, a.owner, a.repository, id, int64(attempt), options)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, page.Jobs...)
		if response.NextPage == 0 {
			return jobs, nil
		}
		options.Page = response.NextPage
	}
}

// JobLog follows the SDK-provided download URL without forwarding API credentials.
func (a *Actions) JobLog(ctx context.Context, id int64) (io.ReadCloser, error) {
	location, _, err := a.service.GetWorkflowJobLogs(ctx, a.owner, a.repository, id, 0)
	if err != nil {
		return nil, err
	}
	if location == nil || location.Scheme != "https" || location.Host == "" || location.User != nil {
		return nil, fmt.Errorf("github: invalid job log download URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("github: job logs returned %s", response.Status)
	}
	return response.Body, nil
}
