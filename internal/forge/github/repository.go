package github

import (
	"fmt"
	"strings"
	"sync"

	"github.com/herbygillot/dockhand/internal/forge"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

const publicInstance = "https://github.com"

// pageSize is how many tags or releases a page of a listing holds: the most
// GitHub's API gives, where it would give 30 unasked. A project with a
// thousand tags is read in ten requests rather than thirty-four.
const pageSize = 100

// A repository is bound for one question, such as one port's newest
// release, and remembers the tags it has read for as long: a tag asked for
// twice, as the newest release and again to date it, is read once.
type repository struct {
	client *Client
	name   string

	mu   sync.Mutex
	tags map[string]forge.Tag
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
