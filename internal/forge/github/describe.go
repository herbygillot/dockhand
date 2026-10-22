package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// Describe reads the repository's description, homepage, and the license
// GitHub detected.
func (r *repository) Describe(ctx context.Context) (forge.Description, error) {
	client, err := r.client.API(ctx)
	if err != nil {
		return forge.Description{}, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	row, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return forge.Description{}, githubapi.RateLimitError(err)
	}
	// A renamed repository answers by its new name, which GitHub's
	// redirect otherwise hides; the caller weighs it.
	return forge.Description{Name: row.GetFullName(), Description: row.GetDescription(), Homepage: row.GetHomepage(), License: row.GetLicense().GetSPDXID()}, nil
}

// Describe reads a repository's description, by its owner/name, as the
// repository it binds says it (repository.Describe).
func (c *Client) Describe(ctx context.Context, instance, name string) (forge.Description, error) {
	if c == nil || instance != publicInstance || !githubapi.ValidRepositoryName(name) {
		return forge.Description{}, fmt.Errorf("github: invalid repository %q", name)
	}
	return (&repository{client: c, name: name}).Describe(ctx)
}
