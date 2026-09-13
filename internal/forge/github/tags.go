package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/git"
)

const (
	tagResponseLimit = 1 << 20
	tagDepthLimit    = 8
	tagLookupTimeout = 30 * time.Second
)

type gitObject struct{ Type, SHA string }

func (r *repository) Tag(ctx context.Context, name string) (forge.Tag, error) {
	if !git.ValidRefName("refs/tags/" + name) {
		return forge.Tag{}, fmt.Errorf("github: invalid tag")
	}
	ctx, cancel := context.WithTimeout(ctx, tagLookupTimeout)
	defer cancel()
	var ref struct {
		Ref    string
		Object gitObject
	}
	resource := "repos/" + r.name + "/git/ref/tags/" + url.PathEscape(name)
	if err := r.client.getJSON(ctx, resource, &ref, tagResponseLimit); err != nil {
		var failure *HTTPError
		if errors.As(err, &failure) && failure.StatusCode == http.StatusNotFound {
			return forge.Tag{}, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return forge.Tag{}, err
	}
	if ref.Ref != "refs/tags/"+name {
		return forge.Tag{}, fmt.Errorf("github: response identifies a different ref")
	}
	object := ref.Object
	seen := map[string]bool{}
	for depth := 0; depth < tagDepthLimit; depth++ {
		if !git.ValidObjectID(object.SHA) || seen[object.SHA] {
			return forge.Tag{}, fmt.Errorf("github: invalid or cyclic tag object")
		}
		seen[object.SHA] = true
		if object.Type == "commit" {
			return forge.Tag{Name: name, Commit: object.SHA}, nil
		}
		if object.Type != "tag" {
			return forge.Tag{}, fmt.Errorf("github: tag does not identify a commit")
		}
		var annotated struct {
			SHA    string
			Object gitObject
		}
		if err := r.client.getJSON(ctx, "repos/"+r.name+"/git/tags/"+object.SHA, &annotated, tagResponseLimit); err != nil {
			return forge.Tag{}, err
		}
		if annotated.SHA != object.SHA {
			return forge.Tag{}, fmt.Errorf("github: response identifies a different tag object")
		}
		object = annotated.Object
	}
	return forge.Tag{}, fmt.Errorf("github: annotated tag nesting exceeds supported depth")
}

func (r *repository) ListTags(ctx context.Context) ([]forge.Tag, error) {
	type item struct {
		Name   string
		Commit struct{ SHA string }
	}
	rows, err := collectPages[item](ctx, r.client, r.name, "tags")
	if err != nil {
		return nil, err
	}
	tags := make([]forge.Tag, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		if !git.ValidRefName("refs/tags/"+row.Name) || !git.ValidObjectID(row.Commit.SHA) {
			return nil, fmt.Errorf("github: invalid repository tag")
		}
		if seen[row.Name] {
			return nil, fmt.Errorf("%w: duplicate tag %s", forge.ErrIncomplete, row.Name)
		}
		seen[row.Name] = true
		tags = append(tags, forge.Tag{Name: row.Name, Commit: row.Commit.SHA})
	}
	return tags, nil
}
