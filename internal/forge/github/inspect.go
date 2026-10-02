package github

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// Inspect reports mergeability, the latest review from each reviewer, check
// runs, and commit statuses for the pull request's current head. It reads
// only; nothing here reruns, comments, or merges.
func (c *Client) Inspect(ctx context.Context, ref forge.PullRequestRef) (forge.PullRequestStatus, error) {
	if ref.Forge != forge.GitHub || !githubapi.ValidRepositoryName(ref.Repository) || ref.Number <= 0 {
		return forge.PullRequestStatus{}, fmt.Errorf("github: invalid pull-request reference")
	}
	client, err := c.API(ctx)
	if err != nil {
		return forge.PullRequestStatus{}, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(ref.Repository, "/")
	row, _, err := client.PullRequests.Get(ctx, owner, repo, ref.Number)
	if err != nil {
		return forge.PullRequestStatus{}, githubapi.RateLimitError(err)
	}
	if row.GetNumber() != ref.Number || row.Head == nil || row.Head.GetSHA() == "" {
		return forge.PullRequestStatus{}, fmt.Errorf("github: response identifies another pull request")
	}
	status := forge.PullRequestStatus{Draft: row.GetDraft(), Mergeable: "unknown", MergeableDetail: row.GetMergeableState(), Review: "none", ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if row.Mergeable != nil {
		status.Mergeable = map[bool]string{true: "yes", false: "no"}[*row.Mergeable]
	}
	if status.MergeableDetail == "unknown" {
		status.MergeableDetail = ""
	}
	latest := map[string]string{}
	submitted := map[string]time.Time{}
	for review, err := range client.PullRequests.ListReviewsIter(ctx, owner, repo, ref.Number, nil) {
		if err != nil {
			return forge.PullRequestStatus{}, githubapi.RateLimitError(err)
		}
		switch review.GetState() {
		case "APPROVED", "CHANGES_REQUESTED":
			latest[review.GetUser().GetLogin()] = review.GetState()
			submitted[review.GetUser().GetLogin()] = review.GetSubmittedAt().Time
		case "DISMISSED":
			delete(latest, review.GetUser().GetLogin())
		}
	}
	for login, state := range latest {
		if state == "APPROVED" {
			status.Approvals++
		} else {
			status.ChangesRequested++
			status.ChangesRequestedBy = append(status.ChangesRequestedBy, login)
		}
	}
	slices.Sort(status.ChangesRequestedBy)
	switch {
	case status.ChangesRequested > 0:
		status.Review = "changes-requested"
	case status.Approvals > 0:
		status.Review = "approved"
	}
	// When the review that sets it was made, which status says rather
	// than when dockhand read it: a refresh made a nine-hour-old request
	// for changes read "just now" (field testing, 2026-10-02).
	for login, state := range latest {
		if (state == "CHANGES_REQUESTED") == (status.Review == "changes-requested") && submitted[login].After(status.ReviewedAt) {
			status.ReviewedAt = submitted[login].UTC()
		}
	}
	head := row.Head.GetSHA()
	for run, err := range client.Checks.ListCheckRunsForRefIter(ctx, owner, repo, head, nil) {
		if err != nil {
			return forge.PullRequestStatus{}, githubapi.RateLimitError(err)
		}
		status.Checks.Total++
		switch {
		case run.GetStatus() != "completed":
			status.Checks.Pending++
		case run.GetConclusion() == "success" || run.GetConclusion() == "neutral" || run.GetConclusion() == "skipped":
			status.Checks.Passed++
		default:
			status.Checks.Failed++
			status.Checks.Failing = append(status.Checks.Failing, run.GetName())
		}
	}
	combined, _, err := client.Repositories.GetCombinedStatus(ctx, owner, repo, head, nil)
	if err != nil {
		return forge.PullRequestStatus{}, githubapi.RateLimitError(err)
	}
	for _, item := range combined.Statuses {
		status.Checks.Total++
		switch item.GetState() {
		case "success":
			status.Checks.Passed++
		case "pending":
			status.Checks.Pending++
		default:
			status.Checks.Failed++
			status.Checks.Failing = append(status.Checks.Failing, item.GetContext())
		}
	}
	slices.Sort(status.Checks.Failing)
	status.Checks.Failing = slices.Compact(status.Checks.Failing)
	return status, nil
}

var _ forge.PullRequestInspector = (*Client)(nil)
