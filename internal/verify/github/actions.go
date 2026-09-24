package github

import (
	"context"
	"fmt"
	"math"
	"github.com/herbygillot/dockhand/internal/fetch"
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
	return &actionsClient{service: api.Actions, owner: owner, repository: name, http: c.HTTP}, nil
}

type actionsClient struct {
	service           *gh.ActionsService
	owner, repository string
	// http downloads what the API only points at, the job logs, with the
	// client the provider was given.
	http *http.Client
}

// maxJobLogBytes is how much of one job's log is kept; GitHub's logs run
// to megabytes, and a bound is what keeps a runaway one off the disk. A
// longer log is kept to the bound and says so (cacheJobLog), rather than
// failing every read.
const maxJobLogBytes = 64 << 20

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

// JobLog follows the SDK-provided download URL without forwarding API
// credentials, through fetch like every other download, on the provider's
// client, with the status handled once. It is not refused by its size: the
// reader keeps what it bounds, and stops reading there.
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
	request.Header.Set("User-Agent", fetch.UserAgent)
	response, err := fetch.Open(a.http, request, math.MaxInt64)
	if err != nil {
		return nil, fmt.Errorf("github: job logs: %w", err)
	}
	return response.Body, nil
}

func (p *Provider) actions(ctx context.Context, repository string) (actionsAPI, error) {
	if p.backend != nil {
		return p.backend(ctx, repository)
	}
	return newActions(ctx, p.Client, repository)
}
