package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v93/github"
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
	var result forge.RepositoryInfo
	if row.GetFork() {
		if row.Parent == nil || !githubapi.ValidRepositoryName(row.Parent.GetFullName()) {
			return result, fmt.Errorf("github: fork parent is unknown")
		}
		result.Parent = row.Parent.GetFullName()
	}
	return result, nil
}

// A permanent rejection settles publication. A rate-limit refusal permits a
// later write; other failures require observation to determine the outcome.
func publicationError(response *gh.Response, err error) error {
	var limited *forge.RateLimitError
	converted := githubapi.RateLimitError(err)
	if errors.As(converted, &limited) {
		return converted
	}
	if errors.Is(err, forge.ErrAuthentication) {
		return fmt.Errorf("%w: %w", forge.ErrRejected, converted)
	}
	if err != nil && response != nil {
		switch response.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity:
			if response.StatusCode == http.StatusUnprocessableEntity && alreadyOpen(err) {
				return fmt.Errorf("%w: %w: %w", forge.ErrPullRequestExists, forge.ErrRejected, err)
			}
			return fmt.Errorf("%w: %w", forge.ErrRejected, err)
		}
	}
	return err
}

// alreadyOpen reports GitHub's refusal of a create because a pull request
// is already open for its head: "A pull request already exists for
// owner:branch.", in the error's list or its message.
func alreadyOpen(err error) bool {
	var response *gh.ErrorResponse
	if !errors.As(err, &response) {
		return false
	}
	words := []string{response.Message}
	for _, e := range response.Errors {
		words = append(words, e.Message)
	}
	for _, w := range words {
		if strings.Contains(strings.ToLower(w), "a pull request already exists") {
			return true
		}
	}
	return false
}
