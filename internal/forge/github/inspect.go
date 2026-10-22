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
	var opinions []opinion
	for login, state := range latest {
		opinions = append(opinions, opinion{Login: login, State: state, At: submitted[login]})
	}
	review(&status, opinions)
	head := row.Head.GetSHA()
	for run, err := range client.Checks.ListCheckRunsForRefIter(ctx, owner, repo, head, nil) {
		if err != nil {
			return forge.PullRequestStatus{}, githubapi.RateLimitError(err)
		}
		check(&status, true, run.GetName(), run.GetStatus(), run.GetConclusion())
	}
	combined, _, err := client.Repositories.GetCombinedStatus(ctx, owner, repo, head, nil)
	if err != nil {
		return forge.PullRequestStatus{}, githubapi.RateLimitError(err)
	}
	for _, item := range combined.Statuses {
		check(&status, false, item.GetContext(), item.GetState(), "")
	}
	slices.Sort(status.Checks.Failing)
	status.Checks.Failing = slices.Compact(status.Checks.Failing)
	return status, nil
}
