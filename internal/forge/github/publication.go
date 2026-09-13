package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"

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
	var row struct {
		Name               string `json:"full_name"`
		DefaultBranch      string `json:"default_branch"`
		Fork               bool
		Archived, Disabled bool
		Parent             *struct {
			Name string `json:"full_name"`
		}
	}
	if err := c.getJSON(ctx, "repos/"+name, &row, 1<<20); err != nil {
		return forge.RepositoryInfo{}, err
	}
	if !strings.EqualFold(row.Name, name) || !git.ValidBranchName(row.DefaultBranch) || row.Archived || row.Disabled {
		return forge.RepositoryInfo{}, fmt.Errorf("github: repository metadata is invalid or repository is archived/disabled")
	}
	result := forge.RepositoryInfo{Name: row.Name, DefaultBranch: row.DefaultBranch, CloneURL: webOrigin + "/" + row.Name + ".git"}
	if row.Fork {
		if row.Parent == nil || !validRepositoryName(row.Parent.Name) {
			return result, fmt.Errorf("github: fork parent is unknown")
		}
		result.Parent = row.Parent.Name
	}
	return result, nil
}

func (c *Client) Name() string { return "github" }
