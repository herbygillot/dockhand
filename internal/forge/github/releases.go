package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v93/github"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
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
		r.answer(row.HTMLURL)
		if row.PublishedAt != nil {
			release.PublishedAt = row.PublishedAt.Time
		}
		releases = append(releases, release)
	}
	return releases, nil
}

// Assets are the files the release of a tag carries, as GitHub's "Get a
// release by tag name" describes them: the name, the browser download URL
// GitHub serves each at, and its size.
func (r *repository) Assets(ctx context.Context, tag string) ([]model.ReleaseAsset, error) {
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
	seen := map[string]bool{}
	var assets []model.ReleaseAsset
	for _, row := range release.Assets {
		name, address := row.GetName(), row.GetBrowserDownloadURL()
		if !forge.AssetName(name) || !forge.ServedURL(address) {
			return nil, fmt.Errorf("%w: release %s describes an asset %q at %q", forge.ErrIncomplete, tag, name, address)
		}
		if seen[name] {
			return nil, fmt.Errorf("%w: release %s carries %s twice", forge.ErrIncomplete, tag, name)
		}
		seen[name] = true
		asset := model.ReleaseAsset{Name: name, URL: address}
		if row.Size != nil {
			asset.Size, asset.SizeKnown = int64(*row.Size), true
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

var _ forge.AssetRepository = (*repository)(nil)
