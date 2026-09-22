package github

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

const publicInstance = "https://github.com"

type repository struct {
	client *Client
	name   string
}

// Repository validates a GitHub owner/name without making a network request.
func (c *Client) Repository(instance, name string) (forge.Repository, error) {
	if c == nil || instance != publicInstance || !githubapi.ValidRepositoryName(name) {
		return nil, fmt.Errorf("github: invalid repository %q", name)
	}
	return &repository{client: c, name: name}, nil
}

func (r *repository) Name() string { return r.name }

// cloneURL is where git reads the repository: the configured clone base, or
// GitHub itself.
func (r *repository) cloneURL() string {
	base := publicInstance
	if r.client.Client != nil && r.client.Config.CloneURL != "" {
		base = strings.TrimSuffix(r.client.Config.CloneURL, "/")
	}
	return base + "/" + r.name + ".git"
}
