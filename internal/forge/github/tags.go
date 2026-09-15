package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
)

func (r *repository) Tag(ctx context.Context, name string) (forge.Tag, error) {
	refName := "refs/tags/" + name
	if !git.ValidRefName(refName) {
		return forge.Tag{}, fmt.Errorf("github: invalid tag")
	}
	client, err := r.client.api(ctx)
	if err != nil {
		return forge.Tag{}, err
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	ref, response, err := client.Git.GetRef(ctx, owner, repo, refName)
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return forge.Tag{}, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return forge.Tag{}, err
	}
	if ref.GetRef() != refName {
		return forge.Tag{}, fmt.Errorf("github: response identifies a different ref")
	}
	object := ref.Object
	seen := map[string]bool{}
	for {
		sha := object.GetSHA()
		if !git.ValidObjectID(sha) || seen[sha] {
			return forge.Tag{}, fmt.Errorf("github: invalid or cyclic tag object")
		}
		seen[sha] = true
		if object.GetType() == "commit" {
			return forge.Tag{Name: name, Commit: sha}, nil
		}
		if object.GetType() != "tag" {
			return forge.Tag{}, fmt.Errorf("github: tag does not identify a commit")
		}
		annotated, _, err := client.Git.GetTag(ctx, owner, repo, sha)
		if err != nil {
			return forge.Tag{}, err
		}
		if annotated.GetSHA() != sha {
			return forge.Tag{}, fmt.Errorf("github: response identifies a different tag object")
		}
		object = annotated.Object
	}
}

func (r *repository) ListTags(ctx context.Context) ([]forge.Tag, error) {
	client, err := r.client.api(ctx)
	if err != nil {
		return nil, err
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	seen := map[string]bool{}
	var tags []forge.Tag
	for row, err := range client.Repositories.ListTagsIter(ctx, owner, repo, nil) {
		if err != nil {
			return nil, err
		}
		if row == nil || !git.ValidRefName("refs/tags/"+row.GetName()) || !git.ValidObjectID(row.GetCommit().GetSHA()) {
			return nil, fmt.Errorf("github: invalid repository tag")
		}
		if seen[row.GetName()] {
			return nil, fmt.Errorf("%w: duplicate tag %s", forge.ErrIncomplete, row.GetName())
		}
		seen[row.GetName()] = true
		tags = append(tags, forge.Tag{Name: row.GetName(), Commit: row.GetCommit().GetSHA()})
	}
	return tags, nil
}
