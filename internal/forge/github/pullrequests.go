package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

func pullRequestObservation(r *gh.PullRequest, repository string) (forge.PullRequestObservation, error) {
	at := time.Now().UTC().Truncate(time.Millisecond)
	if r == nil || r.Head == nil || r.Base == nil || r.GetNumber() <= 0 || r.Head.Repo == nil || r.Base.Repo == nil || !strings.EqualFold(r.Base.Repo.GetFullName(), repository) || !validRepositoryName(r.Head.Repo.GetFullName()) || !git.ValidBranchName(r.Head.GetRef()) || !git.ValidBranchName(r.Base.GetRef()) || !git.ValidObjectID(r.Head.GetSHA()) || (r.GetState() != "open" && r.GetState() != "closed") || r.GetTitle() == "" || r.GetHTMLURL() == "" {
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
	return forge.PullRequestObservation{Found: true, ObservedAt: at, PullRequest: record.PullRequest{Ref: record.PullRequestRef{Forge: "github", Repository: repository, Number: r.GetNumber(), URL: r.GetHTMLURL()}, HeadRepository: r.Head.Repo.GetFullName(), HeadBranch: r.Head.GetRef(), BaseBranch: r.Base.GetRef(), State: state, RemoteHead: record.ObjectID(r.Head.GetSHA()), Title: r.GetTitle(), Body: body, ObservedAt: at}}, nil
}

func validQuery(q forge.PullRequestQuery) bool {
	return validRepositoryName(q.Repository) && validRepositoryName(q.HeadRepository) && git.ValidBranchName(q.HeadBranch) && git.ValidBranchName(q.BaseBranch)
}

func (c *Client) Find(ctx context.Context, q forge.PullRequestQuery) (forge.PullRequestObservation, error) {
	if !validQuery(q) {
		return forge.PullRequestObservation{}, fmt.Errorf("github: invalid pull-request query")
	}
	client, err := c.api(ctx)
	if err != nil {
		return forge.PullRequestObservation{}, err
	}
	owner, repo, _ := strings.Cut(q.Repository, "/")
	headOwner, _, _ := strings.Cut(q.HeadRepository, "/")
	options := &gh.PullRequestListOptions{
		State: "all", Head: headOwner + ":" + q.HeadBranch, Base: q.BaseBranch,
	}
	found := forge.PullRequestObservation{ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}
	for row, err := range client.PullRequests.ListIter(ctx, owner, repo, options) {
		if err != nil {
			return forge.PullRequestObservation{}, err
		}
		observation, err := pullRequestObservation(row, q.Repository)
		if err != nil {
			return forge.PullRequestObservation{}, err
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
	if ref.Forge != "github" || !validRepositoryName(ref.Repository) || ref.Number <= 0 {
		return forge.PullRequestObservation{}, fmt.Errorf("github: invalid pull-request reference")
	}
	client, err := c.api(ctx)
	if err != nil {
		return forge.PullRequestObservation{}, err
	}
	owner, repo, _ := strings.Cut(ref.Repository, "/")
	row, _, err := client.PullRequests.Get(ctx, owner, repo, ref.Number)
	if err != nil {
		return forge.PullRequestObservation{}, err
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
	client, err := c.authenticatedAPI(ctx)
	if err != nil {
		return forge.PullRequestObservation{}, err
	}
	owner, repo, _ := strings.Cut(input.Repository, "/")
	headOwner, headRepo, _ := strings.Cut(input.HeadRepository, "/")
	row, response, err := client.PullRequests.Create(ctx, owner, repo, gh.CreatePullRequest{
		Title: &input.Desired.Title, Body: &input.Desired.Body, Head: headOwner + ":" + input.HeadBranch,
		HeadRepo: &headRepo, Base: input.BaseBranch, MaintainerCanModify: new(true),
	})
	if err := publicationError(response, err); err != nil {
		return forge.PullRequestObservation{}, err
	}
	return pullRequestObservation(row, input.Repository)
}

func (c *Client) Update(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	if input.ExistingPR == nil || input.ExistingPR.Forge != "github" || input.ExistingPR.Repository != input.Repository || input.ExistingPR.Number <= 0 || !validQuery(forge.PullRequestQuery{Repository: input.Repository, HeadRepository: input.HeadRepository, HeadBranch: input.HeadBranch, BaseBranch: input.BaseBranch}) || input.Desired.Title == "" {
		return forge.PullRequestObservation{}, fmt.Errorf("%w: invalid pull-request input", forge.ErrRejected)
	}
	client, err := c.authenticatedAPI(ctx)
	if err != nil {
		return forge.PullRequestObservation{}, err
	}
	owner, repo, _ := strings.Cut(input.Repository, "/")
	row, response, err := client.PullRequests.Edit(ctx, owner, repo, input.ExistingPR.Number, &gh.PullRequest{
		Title: &input.Desired.Title, Body: &input.Desired.Body,
	})
	if err := publicationError(response, err); err != nil {
		return forge.PullRequestObservation{}, err
	}
	if row.GetNumber() != input.ExistingPR.Number {
		return forge.PullRequestObservation{}, fmt.Errorf("github: response identifies another pull request")
	}
	return pullRequestObservation(row, input.Repository)
}
