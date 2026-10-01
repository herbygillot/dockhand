package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

var _ forge.DatedRepository = (*repository)(nil)

// CommitTime is when a commit was made, by its committer's date.
func (r *repository) CommitTime(ctx context.Context, commit string) (time.Time, error) {
	if !git.ValidObjectID(commit) {
		return time.Time{}, fmt.Errorf("github: invalid commit")
	}
	client, err := r.client.API(ctx)
	if err != nil {
		return time.Time{}, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	found, response, err := client.Git.GetCommit(ctx, owner, repo, commit)
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return time.Time{}, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return time.Time{}, githubapi.RateLimitError(err)
	}
	if found.GetSHA() != commit || found.GetCommitter().GetDate().IsZero() {
		return time.Time{}, fmt.Errorf("github: commit %s has no date", commit)
	}
	return found.GetCommitter().GetDate().Time, nil
}

var _ forge.CommitRepository = (*repository)(nil)

// commitQuery finds a commit by a revision expression, which GitHub's
// GraphQL API documents as "suitable for rev-parse": a whole commit name,
// or a leading part of one unique in the repository, as gitrevisions(7)
// has it. Its REST API documents only "a commit SHA", so a Generated-By's
// twelve characters are asked here.
const commitQuery = `query($owner: String!, $name: String!, $expression: String!) { repository(owner: $owner, name: $name) { object(expression: $expression) { ... on Commit { oid } } } }`

// HasCommit reports whether the repository has a commit, named in full or
// abbreviated. GitHub's GraphQL API takes only a signed-in request.
func (r *repository) HasCommit(ctx context.Context, commit string) (bool, error) {
	if !git.ValidObjectID(commit) && !git.ValidAbbreviation(commit) {
		return false, fmt.Errorf("github: invalid commit %q", commit)
	}
	client, err := r.client.AuthenticatedAPI(ctx)
	if err != nil {
		return false, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	request, err := client.NewRequest(ctx, "POST", "graphql", map[string]any{"query": commitQuery,
		"variables": map[string]any{"owner": owner, "name": repo, "expression": commit}})
	if err != nil {
		return false, err
	}
	var result struct {
		Data struct {
			Repository *struct {
				Object *struct {
					OID string `json:"oid"`
				} `json:"object"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	if _, err := client.Do(request, &result); err != nil {
		return false, githubapi.RateLimitError(err)
	}
	if len(result.Errors) > 0 {
		return false, fmt.Errorf("github: %s", result.Errors[0].Message)
	}
	if result.Data.Repository == nil {
		return false, fmt.Errorf("github: no repository %s", r.name)
	}
	// An object that isn't a commit, or one another name led to, isn't
	// the commit asked for.
	object := result.Data.Repository.Object
	return object != nil && strings.HasPrefix(object.OID, commit), nil
}
