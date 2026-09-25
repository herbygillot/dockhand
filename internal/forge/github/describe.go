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
	if !strings.EqualFold(row.GetFullName(), r.name) {
		return forge.Description{}, fmt.Errorf("github: %s answered for %s", r.name, row.GetFullName())
	}
	return forge.Description{Description: row.GetDescription(), Homepage: row.GetHomepage(), License: row.GetLicense().GetSPDXID()}, nil
}
