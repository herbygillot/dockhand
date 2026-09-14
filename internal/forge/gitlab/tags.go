package gitlab

import (
	"context"
	"fmt"
	"net/http"

	sdk "gitlab.com/gitlab-org/api/client-go/v2"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
)

func (r *repository) Tag(ctx context.Context, name string) (forge.Tag, error) {
	if !git.ValidRefName("refs/tags/" + name) {
		return forge.Tag{}, fmt.Errorf("gitlab: invalid tag")
	}
	client, err := r.api()
	if err != nil {
		return forge.Tag{}, err
	}
	row, response, err := client.Tags.GetTag(r.project, name, sdk.WithContext(ctx))
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return forge.Tag{}, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return forge.Tag{}, fmt.Errorf("gitlab: reading tag: %w", err)
	}
	tag, err := observedTag(row)
	if err != nil {
		return forge.Tag{}, err
	}
	if tag.Name != name {
		return forge.Tag{}, fmt.Errorf("gitlab: response identifies a different tag")
	}
	return tag, nil
}

func (r *repository) ListTags(ctx context.Context) ([]forge.Tag, error) {
	client, err := r.api()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var tags []forge.Tag
	for row, err := range sdk.Scan2(func(page sdk.PaginationOptionFunc) ([]*sdk.Tag, *sdk.Response, error) {
		options := []sdk.RequestOptionFunc{sdk.WithContext(ctx)}
		if page != nil {
			options = append(options, page)
		}
		return client.Tags.ListTags(r.project, nil, options...)
	}) {
		if err != nil {
			return nil, fmt.Errorf("gitlab: listing tags: %w", err)
		}
		tag, err := observedTag(row)
		if err != nil {
			return nil, err
		}
		if seen[tag.Name] {
			return nil, fmt.Errorf("%w: duplicate tag %s", forge.ErrIncomplete, tag.Name)
		}
		seen[tag.Name] = true
		tags = append(tags, tag)
	}
	return tags, nil
}

func observedTag(row *sdk.Tag) (forge.Tag, error) {
	if row == nil || !git.ValidRefName("refs/tags/"+row.Name) || row.Commit == nil || !git.ValidObjectID(row.Commit.ID) {
		return forge.Tag{}, fmt.Errorf("gitlab: invalid repository tag")
	}
	return forge.Tag{Name: row.Name, Commit: row.Commit.ID}, nil
}
