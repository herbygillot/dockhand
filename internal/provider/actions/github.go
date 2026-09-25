package actions

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v91/github"

	"github.com/herbygillot/dockhand/internal/fetch"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// maxJobLogBytes bounds one job's log; a longer one is kept to the bound.
const maxJobLogBytes = 64 << 20

// GitHub is the API on GitHub itself, with your login.
type GitHub struct {
	Client *githubapi.Client
}

func (g GitHub) service(ctx context.Context, repository string) (*gh.ActionsService, string, string, error) {
	if !githubapi.ValidRepositoryName(repository) {
		return nil, "", "", fmt.Errorf("github: invalid repository %q", repository)
	}
	api, err := g.Client.AuthenticatedAPI(ctx)
	if err != nil {
		return nil, "", "", githubapi.RateLimitError(err)
	}
	owner, name, _ := strings.Cut(repository, "/")
	return api.Actions, owner, name, nil
}

func run(r *gh.WorkflowRun) Run {
	return Run{ID: r.GetID(), Attempt: r.GetRunAttempt(), Status: r.GetStatus(), Conclusion: r.GetConclusion(), URL: r.GetHTMLURL()}
}

func (g GitHub) Runs(ctx context.Context, repository, branch, commit string) ([]Run, error) {
	service, owner, name, err := g.service(ctx, repository)
	if err != nil {
		return nil, err
	}
	options := &gh.ListWorkflowRunsOptions{Branch: branch, HeadSHA: commit, Event: "push"}
	var runs []Run
	for {
		page, response, err := service.ListWorkflowRunsByFileName(ctx, owner, name, Workflow, options)
		if err != nil {
			return nil, githubapi.RateLimitError(err)
		}
		for _, r := range page.WorkflowRuns {
			runs = append(runs, run(r))
		}
		if response.NextPage == 0 {
			return runs, nil
		}
		options.Page = response.NextPage
	}
}

func (g GitHub) Run(ctx context.Context, repository string, id int64) (Run, error) {
	service, owner, name, err := g.service(ctx, repository)
	if err != nil {
		return Run{}, err
	}
	value, _, err := service.GetWorkflowRunByID(ctx, owner, name, id)
	if err != nil {
		return Run{}, githubapi.RateLimitError(err)
	}
	return run(value), nil
}

func (g GitHub) Rerun(ctx context.Context, repository string, id int64) error {
	service, owner, name, err := g.service(ctx, repository)
	if err != nil {
		return err
	}
	_, err = service.RerunFailedJobsByID(ctx, owner, name, id)
	return githubapi.RateLimitError(err)
}

func (g GitHub) Jobs(ctx context.Context, repository string, id int64, attempt int) ([]RunnerJob, error) {
	service, owner, name, err := g.service(ctx, repository)
	if err != nil {
		return nil, err
	}
	options := &gh.ListOptions{}
	var jobs []RunnerJob
	for {
		page, response, err := service.ListWorkflowJobsAttempt(ctx, owner, name, id, int64(attempt), options)
		if err != nil {
			return nil, githubapi.RateLimitError(err)
		}
		for _, j := range page.Jobs {
			jobs = append(jobs, RunnerJob{ID: j.GetID(), Name: j.GetName(), Status: j.GetStatus(), Conclusion: j.GetConclusion()})
		}
		if response.NextPage == 0 {
			return jobs, nil
		}
		options.Page = response.NextPage
	}
}

// JobLog follows the download URL GitHub gives, without the API's
// credentials.
func (g GitHub) JobLog(ctx context.Context, repository string, job int64) ([]byte, error) {
	service, owner, name, err := g.service(ctx, repository)
	if err != nil {
		return nil, err
	}
	location, _, err := service.GetWorkflowJobLogs(ctx, owner, name, job, 2)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	if location == nil || location.Scheme != "https" || location.Host == "" || location.User != nil {
		return nil, fmt.Errorf("github: invalid job log download URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", fetch.UserAgent)
	// A log past the bound is kept to it, not refused.
	response, err := fetch.Open(g.Client.HTTP, request, math.MaxInt64)
	if err != nil {
		return nil, fmt.Errorf("github: job log: %w", err)
	}
	defer response.Body.Close()
	return io.ReadAll(io.LimitReader(response.Body, maxJobLogBytes))
}
