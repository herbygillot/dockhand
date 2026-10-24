package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	gh "github.com/google/go-github/v93/github"

	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/forge"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// FailedJobs are a head's GitHub Actions jobs that failed, of each run's
// latest attempt, through the documented endpoints: the repository's
// workflow runs for the head, and each run's jobs. It reads only.
func (c *Client) FailedJobs(ctx context.Context, repository, head string) ([]forge.CIJob, error) {
	if !githubapi.ValidRepositoryName(repository) || head == "" {
		return nil, fmt.Errorf("github: invalid CI query")
	}
	client, err := c.API(ctx)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(repository, "/")
	var failed []forge.CIJob
	for run, err := range client.Actions.ListRepositoryWorkflowRunsIter(ctx, owner, repo, &gh.ListWorkflowRunsOptions{HeadSHA: head}) {
		if err != nil {
			return nil, githubapi.RateLimitError(err)
		}
		if run.GetConclusion() != "failure" {
			continue
		}
		for job, err := range client.Actions.ListWorkflowJobsIter(ctx, owner, repo, run.GetID(), nil) {
			if err != nil {
				return nil, githubapi.RateLimitError(err)
			}
			if job.GetConclusion() == "failure" {
				failed = append(failed, forge.CIJob{ID: job.GetID(), Run: run.GetID(), Attempt: int(job.GetRunAttempt()), Name: job.GetName()})
			}
		}
	}
	return failed, nil
}

// logLimit is the most of a job's log read: a MacPorts CI job's is a few
// hundred kilobytes.
const logLimit = 32 << 20

// timestamp is the time GitHub writes before each line of a job's log.
var timestamp = regexp.MustCompile(`(?m)^\d{4}-\d{2}-\d{2}T[\d:.]+Z `)

// JobLog is a job's log, through its documented link ("Download job logs
// for a workflow run"), which redirects to where it's served, fetched as
// fetch fetches; each line without the time GitHub writes before it. One
// GitHub no longer keeps, or won't give this login, is forge.ErrNotFound.
func (c *Client) JobLog(ctx context.Context, repository string, job int64) ([]byte, error) {
	if !githubapi.ValidRepositoryName(repository) || job <= 0 {
		return nil, fmt.Errorf("github: invalid job")
	}
	client, err := c.AuthenticatedAPI(ctx)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(repository, "/")
	link, response, err := client.Actions.GetWorkflowJobLogs(ctx, owner, repo, job, 0)
	if err != nil {
		if response != nil && (response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone || response.StatusCode == http.StatusForbidden) {
			return nil, fmt.Errorf("%w: job %d's log: %w", forge.ErrNotFound, job, githubapi.RateLimitError(err))
		}
		return nil, githubapi.RateLimitError(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", fetch.UserAgent)
	served, err := fetch.Open(c.HTTP, request, logLimit)
	if err != nil {
		var status *fetch.StatusError
		if errors.As(err, &status) && (status.Status == http.StatusNotFound || status.Status == http.StatusGone) {
			return nil, fmt.Errorf("%w: job %d's log: %w", forge.ErrNotFound, job, err)
		}
		return nil, fmt.Errorf("github: fetching job %d's log: %w", job, err)
	}
	defer served.Body.Close()
	var log bytes.Buffer
	if _, err := io.Copy(&log, served.Body); err != nil {
		return nil, fmt.Errorf("github: reading job %d's log: %w", job, err)
	}
	return timestamp.ReplaceAll(log.Bytes(), nil), nil
}

// RerunFailed runs a workflow run's failed jobs again, as the person whose
// login it is, through the documented endpoint ("Re-run failed jobs from a
// workflow run"), which needs write access to the repository: without it
// GitHub answers 403 or 404, which is forge.ErrRejected.
func (c *Client) RerunFailed(ctx context.Context, repository string, run int64) error {
	if !githubapi.ValidRepositoryName(repository) || run <= 0 {
		return fmt.Errorf("github: invalid run")
	}
	client, err := c.AuthenticatedAPI(ctx)
	if err != nil {
		return githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(repository, "/")
	response, err := client.Actions.RerunFailedJobsByID(ctx, owner, repo, run)
	if err != nil {
		err = githubapi.RateLimitError(err)
		if limited := (*forge.RateLimitError)(nil); !errors.As(err, &limited) && response != nil && (response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound) {
			return fmt.Errorf("%w: re-running %s's run %d: %w", forge.ErrRejected, repository, run, err)
		}
		return err
	}
	return nil
}
