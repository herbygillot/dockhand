package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v91/github"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

func (r *repository) Releases(ctx context.Context) ([]forge.Release, error) {
	client, err := r.client.API(ctx)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	seen := map[string]bool{}
	var releases []forge.Release
	for row, err := range client.Repositories.ListReleasesIter(ctx, owner, repo, &gh.ListOptions{PerPage: pageSize}) {
		if err != nil {
			return nil, githubapi.RateLimitError(err)
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

// Assets are the names of the files the release of a tag carries, as
// GitHub's "Get a release by tag name" gives them.
func (r *repository) Assets(ctx context.Context, tag string) ([]string, error) {
	if !git.ValidRefName("refs/tags/" + tag) {
		return nil, fmt.Errorf("github: invalid tag %q", tag)
	}
	client, err := r.client.API(ctx)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	release, response, err := client.Repositories.GetReleaseByTag(ctx, owner, repo, tag)
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return nil, githubapi.RateLimitError(err)
	}
	var names []string
	for _, asset := range release.Assets {
		names = append(names, asset.GetName())
	}
	return names, nil
}

var _ forge.AssetRepository = (*repository)(nil)
