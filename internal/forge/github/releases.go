package github

import (
	"context"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/git"
)

func (r *repository) Releases(ctx context.Context) ([]forge.Release, error) {
	type item struct {
		Tag               string `json:"tag_name"`
		URL               string `json:"html_url"`
		Draft, Prerelease *bool
		PublishedAt       *time.Time `json:"published_at"`
	}
	rows, err := collectPages[item](ctx, r.client, r.name, "releases")
	if err != nil {
		return nil, err
	}
	releases := make([]forge.Release, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if !git.ValidRefName("refs/tags/"+row.Tag) || row.Draft == nil || row.Prerelease == nil || (!*row.Draft && (row.PublishedAt == nil || row.PublishedAt.IsZero())) {
			return nil, fmt.Errorf("github: invalid release observation")
		}
		if seen[row.Tag] {
			return nil, fmt.Errorf("%w: duplicate release tag %s", forge.ErrIncomplete, row.Tag)
		}
		seen[row.Tag] = true
		release := forge.Release{Tag: row.Tag, URL: row.URL, Draft: *row.Draft, Prerelease: *row.Prerelease}
		if row.PublishedAt != nil {
			release.PublishedAt = *row.PublishedAt
		}
		releases = append(releases, release)
	}
	return releases, nil
}
