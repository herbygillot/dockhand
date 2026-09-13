package github

import (
	"context"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
)

const catalogPages = 20
const catalogPageSize = 100

func collectPages[T any](ctx context.Context, c *Client, repository, kind string) ([]T, error) {
	if !upstream.RepositoryName(repository) {
		return nil, fmt.Errorf("github: invalid repository")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var all []T
	for page := 1; page <= catalogPages; page++ {
		var rows []T
		resource := fmt.Sprintf("repos/%s/%s?per_page=%d&page=%d", repository, kind, catalogPageSize, page)
		if err := c.getJSON(ctx, resource, &rows, 8<<20); err != nil {
			return nil, err
		}
		if rows == nil || len(rows) > catalogPageSize {
			return nil, fmt.Errorf("github: invalid %s page", kind)
		}
		all = append(all, rows...)
		if len(rows) < catalogPageSize {
			return all, nil
		}
	}
	return nil, fmt.Errorf("%w: %s exceeds %d pages", upstream.ErrIncomplete, kind, catalogPages)
}

func (c *Client) Releases(ctx context.Context, repository string) ([]upstream.Release, error) {
	type item struct {
		Tag               string `json:"tag_name"`
		URL               string `json:"html_url"`
		Draft, Prerelease *bool
		PublishedAt       *time.Time `json:"published_at"`
	}
	rows, err := collectPages[item](ctx, c, repository, "releases")
	if err != nil {
		return nil, err
	}
	releases := make([]upstream.Release, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if !git.ValidRefName("refs/tags/"+row.Tag) || row.Draft == nil || row.Prerelease == nil || (!*row.Draft && (row.PublishedAt == nil || row.PublishedAt.IsZero())) {
			return nil, fmt.Errorf("github: invalid release observation")
		}
		if seen[row.Tag] {
			return nil, fmt.Errorf("%w: duplicate release tag %s", upstream.ErrIncomplete, row.Tag)
		}
		seen[row.Tag] = true
		release := upstream.Release{Tag: row.Tag, URL: row.URL, Draft: *row.Draft, Prerelease: *row.Prerelease}
		if row.PublishedAt != nil {
			release.PublishedAt = *row.PublishedAt
		}
		releases = append(releases, release)
	}
	return releases, nil
}

func (c *Client) ListTags(ctx context.Context, repository string) ([]upstream.Tag, error) {
	type item struct {
		Name   string
		Commit struct{ SHA string }
	}
	rows, err := collectPages[item](ctx, c, repository, "tags")
	if err != nil {
		return nil, err
	}
	tags := make([]upstream.Tag, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if !git.ValidRefName("refs/tags/"+row.Name) || !git.ValidObjectID(row.Commit.SHA) {
			return nil, fmt.Errorf("github: invalid repository tag")
		}
		if seen[row.Name] {
			return nil, fmt.Errorf("%w: duplicate tag %s", upstream.ErrIncomplete, row.Name)
		}
		seen[row.Name] = true
		tags = append(tags, upstream.Tag{Name: row.Name, Commit: row.Commit.SHA})
	}
	return tags, nil
}
