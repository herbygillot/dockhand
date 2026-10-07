package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// NameFromRemote is the repository a remote's URL names on GitHub.
func (c *Client) NameFromRemote(remote string) (string, error) {
	return githubapi.RemoteRepository(remote)
}

func (c *Client) RepositoryInfo(ctx context.Context, name string) (forge.RepositoryInfo, error) {
	if !githubapi.ValidRepositoryName(name) {
		return forge.RepositoryInfo{}, fmt.Errorf("github: invalid repository")
	}
	client, err := c.API(ctx)
	if err != nil {
		return forge.RepositoryInfo{}, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(name, "/")
	row, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return forge.RepositoryInfo{}, githubapi.RateLimitError(err)
	}
	// Each refusal says which it is: one message for all four read a
	// renamed fork as invalid, archived, or disabled (the rc8 full stage's
	// F3).
	switch {
	case !strings.EqualFold(row.GetFullName(), name) && githubapi.ValidRepositoryName(row.GetFullName()):
		return forge.RepositoryInfo{}, &forge.RepositoryMovedError{From: name, To: row.GetFullName(), CloneURL: row.GetCloneURL()}
	case !strings.EqualFold(row.GetFullName(), name):
		return forge.RepositoryInfo{}, fmt.Errorf("github: GitHub answered %s with a repository named %q, which isn't one", name, row.GetFullName())
	case !git.ValidBranchName(row.GetDefaultBranch()):
		return forge.RepositoryInfo{}, fmt.Errorf("github: %s's default branch, %q, isn't a branch name dockhand can use", name, row.GetDefaultBranch())
	case row.GetArchived():
		return forge.RepositoryInfo{}, fmt.Errorf("github: %s is archived on GitHub, so nothing can be pushed to it", name)
	case row.GetDisabled():
		return forge.RepositoryInfo{}, fmt.Errorf("github: %s is disabled on GitHub", name)
	}
	cloneName, err := c.NameFromRemote(row.GetCloneURL())
	if err != nil || !strings.EqualFold(cloneName, name) {
		return forge.RepositoryInfo{}, fmt.Errorf("github: clone URL does not identify the repository")
	}
	result := forge.RepositoryInfo{Name: row.GetFullName(), DefaultBranch: row.GetDefaultBranch(), CloneURL: row.GetCloneURL()}
	if row.GetFork() {
		if row.Parent == nil || !githubapi.ValidRepositoryName(row.Parent.GetFullName()) {
			return result, fmt.Errorf("github: fork parent is unknown")
		}
		result.Parent = row.Parent.GetFullName()
	}
	return result, nil
}

func (c *Client) Name() string { return forge.GitHub }

// A permanent rejection settles publication. A rate-limit refusal permits a
// later write; other failures require observation to determine the outcome.
func publicationError(response *gh.Response, err error) error {
	var limited *forge.RateLimitError
	if converted := githubapi.RateLimitError(err); errors.As(converted, &limited) {
		return converted
	}
	if errors.Is(err, forge.ErrAuthentication) {
		return fmt.Errorf("%w: %w", forge.ErrRejected, err)
	}
	if err != nil && response != nil {
		switch response.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity:
			return fmt.Errorf("%w: %w", forge.ErrRejected, err)
		}
	}
	return err
}
