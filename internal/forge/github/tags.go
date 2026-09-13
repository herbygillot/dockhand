package github

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
)

type gitObject struct{ Type, SHA string }

func (c *Client) Tag(ctx context.Context, repository, name string) (upstream.Tag, error) {
	if !upstream.RepositoryName(repository) || !git.ValidRefName("refs/tags/"+name) {
		return upstream.Tag{}, fmt.Errorf("github: invalid repository or tag")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var ref struct {
		Ref    string
		Object gitObject
	}
	resource := "repos/" + repository + "/git/ref/tags/" + url.PathEscape(name)
	if err := c.get(ctx, resource, &ref); err != nil {
		return upstream.Tag{}, err
	}
	if ref.Ref != "refs/tags/"+name {
		return upstream.Tag{}, fmt.Errorf("github: response identifies a different ref")
	}
	object := ref.Object
	seen := map[string]bool{}
	for depth := 0; depth < 8; depth++ {
		if !git.ValidObjectID(object.SHA) || seen[object.SHA] {
			return upstream.Tag{}, fmt.Errorf("github: invalid or cyclic tag object")
		}
		seen[object.SHA] = true
		if object.Type == "commit" {
			return upstream.Tag{Name: name, Commit: object.SHA}, nil
		}
		if object.Type != "tag" {
			return upstream.Tag{}, fmt.Errorf("github: tag does not identify a commit")
		}
		var annotated struct {
			SHA    string
			Object gitObject
		}
		if err := c.get(ctx, "repos/"+repository+"/git/tags/"+object.SHA, &annotated); err != nil {
			return upstream.Tag{}, err
		}
		if annotated.SHA != object.SHA {
			return upstream.Tag{}, fmt.Errorf("github: response identifies a different tag object")
		}
		object = annotated.Object
	}
	return upstream.Tag{}, fmt.Errorf("github: annotated tag nesting exceeds supported depth")
}

var _ upstream.TagReader = (*Client)(nil)
