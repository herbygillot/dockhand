package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

type pullRequestRow struct {
	Number       int
	URL          string `json:"html_url"`
	State, Title string
	Body         *string
	MergedAt     *time.Time `json:"merged_at"`
	Head, Base   struct {
		Ref, SHA string
		Repo     *struct {
			Name string `json:"full_name"`
		}
	}
}

func (r pullRequestRow) observation(repository string) (forge.PullRequestObservation, error) {
	at := time.Now().UTC().Truncate(time.Millisecond)
	if r.Number <= 0 || r.Head.Repo == nil || r.Base.Repo == nil || !strings.EqualFold(r.Base.Repo.Name, repository) || !validRepositoryName(r.Head.Repo.Name) || !git.ValidBranchName(r.Head.Ref) || !git.ValidBranchName(r.Base.Ref) || !git.ValidObjectID(r.Head.SHA) || (r.State != "open" && r.State != "closed") || r.Title == "" {
		return forge.PullRequestObservation{}, fmt.Errorf("github: invalid pull-request observation")
	}
	expectedURL := fmt.Sprintf("%s/%s/pull/%d", webOrigin, repository, r.Number)
	if !strings.EqualFold(r.URL, expectedURL) {
		return forge.PullRequestObservation{}, fmt.Errorf("github: pull-request URL identifies another resource")
	}
	state := record.PullRequestOpen
	if r.State == "closed" {
		state = record.PullRequestClosed
	}
	if r.MergedAt != nil {
		state = record.PullRequestMerged
	}
	body := ""
	if r.Body != nil {
		body = *r.Body
	}
	return forge.PullRequestObservation{Found: true, ObservedAt: at, PullRequest: record.PullRequest{Ref: record.PullRequestRef{Forge: "github", Repository: repository, Number: r.Number, URL: r.URL}, HeadRepository: r.Head.Repo.Name, HeadBranch: r.Head.Ref, BaseBranch: r.Base.Ref, State: state, RemoteHead: record.ObjectID(r.Head.SHA), Title: r.Title, Body: body, ObservedAt: at}}, nil
}

func validQuery(q forge.PullRequestQuery) bool {
	return validRepositoryName(q.Repository) && validRepositoryName(q.HeadRepository) && git.ValidBranchName(q.HeadBranch) && git.ValidBranchName(q.BaseBranch)
}

func (c *Client) Find(ctx context.Context, q forge.PullRequestQuery) (forge.PullRequestObservation, error) {
	if !validQuery(q) {
		return forge.PullRequestObservation{}, fmt.Errorf("github: invalid pull-request query")
	}
	owner, _, _ := strings.Cut(q.HeadRepository, "/")
	query := url.Values{"state": {"all"}, "head": {owner + ":" + q.HeadBranch}, "base": {q.BaseBranch}, "per_page": {"100"}, "sort": {"created"}, "direction": {"desc"}}
	var rows []pullRequestRow
	if err := c.getJSON(ctx, "repos/"+q.Repository+"/pulls?"+query.Encode(), &rows, catalogResponseLimit); err != nil {
		return forge.PullRequestObservation{}, err
	}
	if rows == nil || len(rows) >= 100 {
		return forge.PullRequestObservation{}, fmt.Errorf("%w: pull-request lookup is incomplete", forge.ErrIncomplete)
	}
	found := forge.PullRequestObservation{ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}
	for _, row := range rows {
		observation, err := row.observation(q.Repository)
		if err != nil {
			return found, err
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
	var row pullRequestRow
	if err := c.getJSON(ctx, fmt.Sprintf("repos/%s/pulls/%d", ref.Repository, ref.Number), &row, catalogResponseLimit); err != nil {
		return forge.PullRequestObservation{}, err
	}
	if row.Number != ref.Number {
		return forge.PullRequestObservation{}, fmt.Errorf("github: response identifies another pull request")
	}
	return row.observation(ref.Repository)
}

func (c *Client) Create(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	if !validQuery(forge.PullRequestQuery{Repository: input.Repository, HeadRepository: input.HeadRepository, HeadBranch: input.HeadBranch, BaseBranch: input.BaseBranch}) || input.Desired.Title == "" || input.ExistingPR != nil {
		return forge.PullRequestObservation{}, fmt.Errorf("%w: invalid pull-request input", forge.ErrRejected)
	}
	owner, repo, _ := strings.Cut(input.HeadRepository, "/")
	payload := map[string]any{"title": input.Desired.Title, "body": input.Desired.Body, "head": owner + ":" + input.HeadBranch, "head_repo": repo, "base": input.BaseBranch, "maintainer_can_modify": true}
	var row pullRequestRow
	if err := c.requestJSON(ctx, http.MethodPost, "repos/"+input.Repository+"/pulls", payload, &row, catalogResponseLimit); err != nil {
		return forge.PullRequestObservation{}, err
	}
	return row.observation(input.Repository)
}

func (c *Client) Update(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	if input.ExistingPR == nil || input.ExistingPR.Forge != "github" || input.ExistingPR.Repository != input.Repository || input.ExistingPR.Number <= 0 || !validQuery(forge.PullRequestQuery{Repository: input.Repository, HeadRepository: input.HeadRepository, HeadBranch: input.HeadBranch, BaseBranch: input.BaseBranch}) || input.Desired.Title == "" {
		return forge.PullRequestObservation{}, fmt.Errorf("%w: invalid pull-request input", forge.ErrRejected)
	}
	var row pullRequestRow
	resource := fmt.Sprintf("repos/%s/pulls/%d", input.Repository, input.ExistingPR.Number)
	if err := c.requestJSON(ctx, http.MethodPatch, resource, map[string]string{"title": input.Desired.Title, "body": input.Desired.Body}, &row, catalogResponseLimit); err != nil {
		return forge.PullRequestObservation{}, err
	}
	if row.Number != input.ExistingPR.Number {
		return forge.PullRequestObservation{}, fmt.Errorf("github: response identifies another pull request")
	}
	return row.observation(input.Repository)
}
