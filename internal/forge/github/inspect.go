package github

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/herbygillot/dockhand/internal/forge"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
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
	status := forge.PullRequestStatus{Draft: row.GetDraft(), Head: model.ObjectID(row.Head.GetSHA()), Mergeable: "unknown", MergeableDetail: row.GetMergeableState(), Review: "none"}
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
		// Each login's own latest review, of any kind.
		if login, at := review.GetUser().GetLogin(), review.GetSubmittedAt().Time; login != "" && !at.IsZero() {
			if status.LatestReview == nil {
				status.LatestReview = map[string]time.Time{}
			}
			if at.After(status.LatestReview[login]) {
				status.LatestReview[login] = at.UTC()
			}
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
	// The authenticated account, not the pull request author, decides whose
	// latest human comment is awaiting an answer. If it cannot be learned,
	// the conversation answer remains unknown (#248).
	account, err := c.AuthenticatedUser(ctx)
	if err == nil && account != "" {
		opts := &gh.IssueListCommentsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
		var all []forge.Comment
		for {
			comments, response, err := client.Issues.ListComments(ctx, owner, repo, ref.Number, opts)
			if err != nil {
				return forge.PullRequestStatus{}, githubapi.RateLimitError(err)
			}
			for _, c := range comments {
				comment := forge.Comment{At: c.GetCreatedAt().Time, Kind: forge.CommentUnknown}
				if user := c.GetUser(); user != nil {
					comment.Login = user.GetLogin()
					switch user.GetType() {
					case "User":
						comment.Kind = forge.CommentUser
					case "Bot":
						comment.Kind = forge.CommentBot
					}
				}
				all = append(all, comment)
			}
			if response.NextPage == 0 {
				break
			}
			opts.Page = response.NextPage
		}
		status.CommentedBy, status.CommentedAt, status.CommentsRead = forge.AwaitingResponse(all, account, true)
	}
	// The reviews still requested: GitHub's whole list, which the endpoint
	// gives in one page. A team requested is named by its slug.
	requested, _, err := client.PullRequests.ListReviewers(ctx, owner, repo, ref.Number)
	if err != nil {
		return forge.PullRequestStatus{}, githubapi.RateLimitError(err)
	}
	for _, user := range requested.Users {
		if login := user.GetLogin(); login != "" {
			status.Requested = append(status.Requested, login)
		}
	}
	slices.Sort(status.Requested)
	status.RequestedRead = true
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
