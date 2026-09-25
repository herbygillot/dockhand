package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/record"
)

func pullRequestObservation(r *gh.PullRequest, repository string) (forge.PullRequestObservation, error) {
	at := time.Now().UTC().Truncate(time.Millisecond)
	if r == nil || r.Head == nil || r.Base == nil || r.GetNumber() <= 0 || (r.Head.Repo == nil && r.GetState() != "closed") || r.Base.Repo == nil || !strings.EqualFold(r.Base.Repo.GetFullName(), repository) || (r.Head.Repo != nil && !githubapi.ValidRepositoryName(r.Head.Repo.GetFullName())) || !git.ValidBranchName(r.Head.GetRef()) || !git.ValidBranchName(r.Base.GetRef()) || !git.ValidObjectID(r.Head.GetSHA()) || (r.GetState() != "open" && r.GetState() != "closed") || r.GetTitle() == "" || r.GetHTMLURL() == "" {
		return forge.PullRequestObservation{}, fmt.Errorf("github: invalid pull-request observation")
	}
	state := record.PullRequestOpen
	if r.GetState() == "closed" {
		state = record.PullRequestClosed
	}
	if r.MergedAt != nil {
		state = record.PullRequestMerged
	}
	body := ""
	if r.Body != nil {
		body = *r.Body
	}
	return forge.PullRequestObservation{Found: true, ObservedAt: at, PullRequest: record.PullRequest{Ref: record.PullRequestRef{Forge: forge.GitHub, Repository: repository, Number: r.GetNumber(), URL: r.GetHTMLURL()}, HeadRepository: r.Head.Repo.GetFullName(), HeadBranch: r.Head.GetRef(), BaseBranch: r.Base.GetRef(), State: state, RemoteHead: record.ObjectID(r.Head.GetSHA()), Title: r.GetTitle(), Body: body, Author: r.GetUser().GetLogin(), MaintainerCanModify: r.GetMaintainerCanModify(), ObservedAt: at}}, nil
}

func validQuery(q forge.PullRequestQuery) bool {
	return githubapi.ValidRepositoryName(q.Repository) && githubapi.ValidRepositoryName(q.HeadRepository) && git.ValidBranchName(q.HeadBranch) && git.ValidBranchName(q.BaseBranch)
}

func (c *Client) Find(ctx context.Context, q forge.PullRequestQuery) (forge.PullRequestObservation, error) {
	if !validQuery(q) {
		return forge.PullRequestObservation{}, fmt.Errorf("github: invalid pull-request query")
	}
	client, err := c.API(ctx)
	if err != nil {
		return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(q.Repository, "/")
	headOwner, _, _ := strings.Cut(q.HeadRepository, "/")
	options := &gh.PullRequestListOptions{
		State: "all", Head: headOwner + ":" + q.HeadBranch, Base: q.BaseBranch,
	}
	found := forge.PullRequestObservation{ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}
	for row, err := range client.PullRequests.ListIter(ctx, owner, repo, options) {
		if err != nil {
			return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
		}
		observation, err := pullRequestObservation(row, q.Repository)
		if err != nil {
			return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
		}
		pr := observation.PullRequest
		if !strings.EqualFold(pr.HeadRepository, q.HeadRepository) || pr.HeadBranch != q.HeadBranch || pr.BaseBranch != q.BaseBranch {
			continue
		}
		if found.Found {
			return forge.PullRequestObservation{}, fmt.Errorf("github: multiple pull requests match this branch")
		}
		found = observation
	}
	return found, nil
}

func (c *Client) Observe(ctx context.Context, ref record.PullRequestRef) (forge.PullRequestObservation, error) {
	if ref.Forge != forge.GitHub || !githubapi.ValidRepositoryName(ref.Repository) || ref.Number <= 0 {
		return forge.PullRequestObservation{}, fmt.Errorf("github: invalid pull-request reference")
	}
	client, err := c.API(ctx)
	if err != nil {
		return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(ref.Repository, "/")
	row, _, err := client.PullRequests.Get(ctx, owner, repo, ref.Number)
	if err != nil {
		return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
	}
	if row.GetNumber() != ref.Number {
		return forge.PullRequestObservation{}, fmt.Errorf("github: response identifies another pull request")
	}
	return pullRequestObservation(row, ref.Repository)
}

func (c *Client) Create(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	if !validQuery(forge.PullRequestQuery{Repository: input.Repository, HeadRepository: input.HeadRepository, HeadBranch: input.HeadBranch, BaseBranch: input.BaseBranch}) || input.Desired.Title == "" || input.ExistingPR != nil {
		return forge.PullRequestObservation{}, fmt.Errorf("%w: invalid pull-request input", forge.ErrRejected)
	}
	client, err := c.AuthenticatedAPI(ctx)
	if err != nil {
		return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(input.Repository, "/")
	headOwner, headRepo, _ := strings.Cut(input.HeadRepository, "/")
	row, response, err := client.PullRequests.Create(ctx, owner, repo, gh.CreatePullRequest{
		Title: &input.Desired.Title, Body: &input.Desired.Body, Head: headOwner + ":" + input.HeadBranch,
		HeadRepo: &headRepo, Base: input.BaseBranch, MaintainerCanModify: new(true), Draft: &input.Draft,
	})
	if err := publicationError(response, err); err != nil {
		return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
	}
	return pullRequestObservation(row, input.Repository)
}

func (c *Client) Update(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	if input.ExistingPR == nil || input.ExistingPR.Forge != forge.GitHub || input.ExistingPR.Repository != input.Repository || input.ExistingPR.Number <= 0 || !validQuery(forge.PullRequestQuery{Repository: input.Repository, HeadRepository: input.HeadRepository, HeadBranch: input.HeadBranch, BaseBranch: input.BaseBranch}) || input.Desired.Title == "" {
		return forge.PullRequestObservation{}, fmt.Errorf("%w: invalid pull-request input", forge.ErrRejected)
	}
	client, err := c.AuthenticatedAPI(ctx)
	if err != nil {
		return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(input.Repository, "/")
	row, response, err := client.PullRequests.Edit(ctx, owner, repo, input.ExistingPR.Number, &gh.PullRequest{
		Title: &input.Desired.Title, Body: &input.Desired.Body,
	})
	if err := publicationError(response, err); err != nil {
		return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
	}
	if row.GetNumber() != input.ExistingPR.Number {
		return forge.PullRequestObservation{}, fmt.Errorf("github: response identifies another pull request")
	}
	return pullRequestObservation(row, input.Repository)
}

// OpenPullRequests finds a repository's open pull requests whose titles
// name a port the way a MacPorts subject does, "port:" or "port,".
func (c *Client) OpenPullRequests(ctx context.Context, repository, port string) ([]forge.PullRequestSummary, error) {
	if !githubapi.ValidRepositoryName(repository) || port == "" || strings.ContainsAny(port, "\" \t\r\n") {
		return nil, fmt.Errorf("github: invalid pull-request search")
	}
	client, err := c.API(ctx)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	query := fmt.Sprintf("repo:%s is:pr is:open in:title %q", repository, port)
	result, _, err := client.Search.Issues(ctx, query, &gh.SearchOptions{ListOptions: gh.ListOptions{PerPage: 50}})
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	var found []forge.PullRequestSummary
	for _, issue := range result.Issues {
		title := issue.GetTitle()
		if !issue.IsPullRequest() || !namesPort(title, port) {
			continue
		}
		found = append(found, forge.PullRequestSummary{Number: issue.GetNumber(), Title: title, URL: issue.GetHTMLURL()})
	}
	return found, nil
}

// namesPort reports whether a title begins with a port list, "a, b: …",
// that includes the port.
func namesPort(title, port string) bool {
	prefix, _, ok := strings.Cut(title, ":")
	if !ok {
		return false
	}
	for name := range strings.SplitSeq(prefix, ",") {
		if strings.TrimSpace(name) == port {
			return true
		}
	}
	return false
}

// readyMutation takes a draft out of draft; GitHub's REST API can't.
const readyMutation = `mutation($id: ID!) { markPullRequestReadyForReview(input: {pullRequestId: $id}) { pullRequest { isDraft } } }`

// MarkReady takes a draft pull request out of draft, so it is ready for
// review, and reports it as it then is. One already ready is left alone.
func (c *Client) MarkReady(ctx context.Context, ref record.PullRequestRef) (forge.PullRequestObservation, error) {
	if ref.Forge != forge.GitHub || !githubapi.ValidRepositoryName(ref.Repository) || ref.Number <= 0 {
		return forge.PullRequestObservation{}, fmt.Errorf("github: invalid pull-request reference")
	}
	client, err := c.AuthenticatedAPI(ctx)
	if err != nil {
		return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(ref.Repository, "/")
	row, _, err := client.PullRequests.Get(ctx, owner, repo, ref.Number)
	if err != nil {
		return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
	}
	if row.GetNumber() != ref.Number {
		return forge.PullRequestObservation{}, fmt.Errorf("github: response identifies another pull request")
	}
	if row.GetDraft() {
		request, err := client.NewRequest(ctx, "POST", "graphql", map[string]any{"query": readyMutation, "variables": map[string]any{"id": row.GetNodeID()}})
		if err != nil {
			return forge.PullRequestObservation{}, err
		}
		var result struct {
			Errors []struct{ Message string }
		}
		if _, err := client.Do(request, &result); err != nil {
			return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
		}
		if len(result.Errors) > 0 {
			return forge.PullRequestObservation{}, fmt.Errorf("github: marking #%d ready: %s", ref.Number, result.Errors[0].Message)
		}
		if row, _, err = client.PullRequests.Get(ctx, owner, repo, ref.Number); err != nil {
			return forge.PullRequestObservation{}, githubapi.RateLimitError(err)
		}
	}
	return pullRequestObservation(row, ref.Repository)
}

// Permission is a person's role on a repository: admin, maintain, write,
// triage, read, or none.
func (c *Client) Permission(ctx context.Context, repository, login string) (string, error) {
	if !githubapi.ValidRepositoryName(repository) || login == "" {
		return "", fmt.Errorf("github: invalid permission query")
	}
	client, err := c.AuthenticatedAPI(ctx)
	if err != nil {
		return "", githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(repository, "/")
	level, _, err := client.Repositories.GetPermissionLevel(ctx, owner, repo, login)
	if err != nil {
		return "", githubapi.RateLimitError(err)
	}
	if role := level.GetRoleName(); role != "" {
		return role, nil
	}
	return level.GetPermission(), nil
}

// PostReview posts a review on a pull request at the commit it reviewed,
// and returns the review's address.
func (c *Client) PostReview(ctx context.Context, input forge.ReviewInput) (string, error) {
	ref := input.Ref
	if ref.Forge != forge.GitHub || !githubapi.ValidRepositoryName(ref.Repository) || ref.Number <= 0 || input.Body == "" || !git.ValidObjectID(input.Commit) {
		return "", fmt.Errorf("%w: invalid review", forge.ErrRejected)
	}
	client, err := c.AuthenticatedAPI(ctx)
	if err != nil {
		return "", githubapi.RateLimitError(err)
	}
	event := "COMMENT"
	if input.RequestChanges {
		event = "REQUEST_CHANGES"
	}
	request := &gh.PullRequestReviewRequest{CommitID: &input.Commit, Body: &input.Body, Event: &event}
	for _, comment := range input.Comments {
		request.Comments = append(request.Comments, &gh.DraftReviewComment{Path: new(comment.Path), Line: new(comment.Line), Side: new("RIGHT"), Body: new(comment.Body)})
	}
	owner, repo, _ := strings.Cut(ref.Repository, "/")
	review, response, err := client.PullRequests.CreateReview(ctx, owner, repo, ref.Number, request)
	if err := publicationError(response, err); err != nil {
		return "", githubapi.RateLimitError(err)
	}
	return review.GetHTMLURL(), nil
}

// RequestReviewers asks people to review a pull request again, as GitHub's
// "re-request review" does.
func (c *Client) RequestReviewers(ctx context.Context, ref record.PullRequestRef, logins []string) error {
	if ref.Forge != forge.GitHub || !githubapi.ValidRepositoryName(ref.Repository) || ref.Number <= 0 || len(logins) == 0 {
		return fmt.Errorf("%w: invalid review request", forge.ErrRejected)
	}
	client, err := c.AuthenticatedAPI(ctx)
	if err != nil {
		return githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(ref.Repository, "/")
	_, response, err := client.PullRequests.RequestReviewers(ctx, owner, repo, ref.Number, gh.ReviewersRequest{Reviewers: logins})
	return githubapi.RateLimitError(publicationError(response, err))
}
