package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
)

func (r *repository) Releases(ctx context.Context) ([]forge.Release, error) {
	client, err := r.client.api(ctx)
	if err != nil {
		return nil, err
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	seen := map[string]bool{}
	var releases []forge.Release
	for row, err := range client.Repositories.ListReleasesIter(ctx, owner, repo, nil) {
		if err != nil {
			return nil, err
		}
		if row == nil || !git.ValidRefName("refs/tags/"+row.TagName) || (!row.Draft && (row.PublishedAt == nil || row.PublishedAt.IsZero())) {
			return nil, fmt.Errorf("github: invalid release observation")
		}
		if seen[row.TagName] {
			return nil, fmt.Errorf("%w: duplicate release tag %s", forge.ErrIncomplete, row.TagName)
		}
		seen[row.TagName] = true
		release := forge.Release{Tag: row.TagName, URL: row.HTMLURL, Draft: row.Draft, Prerelease: row.Prerelease}
		if row.PublishedAt != nil {
			release.PublishedAt = row.PublishedAt.Time
		}
		releases = append(releases, release)
	}
	return releases, nil
}
