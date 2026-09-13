package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	gh "github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/git"
)

func (c *Client) NameFromRemote(remote string) (string, error) {
	var name string
	if strings.HasPrefix(remote, "git@github.com:") {
		name = strings.TrimPrefix(remote, "git@github.com:")
	} else {
		u, err := url.Parse(remote)
		if err != nil || !strings.EqualFold(u.Hostname(), "github.com") || u.RawQuery != "" || u.Fragment != "" || (u.Port() != "" && !(u.Scheme == "ssh" && u.Port() == "22")) {
			return "", fmt.Errorf("github: remote must identify a github.com repository")
		}
		if u.Scheme == "https" && u.User != nil || u.Scheme == "ssh" && (u.User == nil || u.User.String() != "git") || u.Scheme != "https" && u.Scheme != "ssh" {
			return "", fmt.Errorf("github: unsupported GitHub remote URL")
		}
		name = strings.TrimPrefix(u.Path, "/")
	}
	name = strings.TrimSuffix(name, ".git")
	if !validRepositoryName(name) {
		return "", fmt.Errorf("github: invalid remote repository")
	}
	return name, nil
}

func (c *Client) RepositoryInfo(ctx context.Context, name string) (forge.RepositoryInfo, error) {
	if !validRepositoryName(name) {
		return forge.RepositoryInfo{}, fmt.Errorf("github: invalid repository")
	}
	client, err := c.api()
	if err != nil {
		return forge.RepositoryInfo{}, err
	}
	owner, repo, _ := strings.Cut(name, "/")
	row, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return forge.RepositoryInfo{}, err
	}
	if !strings.EqualFold(row.GetFullName(), name) || !git.ValidBranchName(row.GetDefaultBranch()) || row.GetArchived() || row.GetDisabled() {
		return forge.RepositoryInfo{}, fmt.Errorf("github: repository metadata is invalid or repository is archived/disabled")
	}
	cloneName, err := c.NameFromRemote(row.GetCloneURL())
	if err != nil || !strings.EqualFold(cloneName, name) {
		return forge.RepositoryInfo{}, fmt.Errorf("github: clone URL does not identify the repository")
	}
	result := forge.RepositoryInfo{Name: row.GetFullName(), DefaultBranch: row.GetDefaultBranch(), CloneURL: row.GetCloneURL()}
	if row.GetFork() {
		if row.Parent == nil || !validRepositoryName(row.Parent.GetFullName()) {
			return result, fmt.Errorf("github: fork parent is unknown")
		}
		result.Parent = row.Parent.GetFullName()
	}
	return result, nil
}

func (c *Client) Name() string { return "github" }

// A rejection settles a publication attempt; other errors require observation
// before the workflow can decide whether a write took effect.
func publicationError(response *gh.Response, err error) error {
	if err != nil && response != nil {
		switch response.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity:
			return fmt.Errorf("%w: %w", forge.ErrRejected, err)
		}
	}
	return err
}
