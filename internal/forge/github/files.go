package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// File reads one file at a commit through the contents API, which returns
// files up to one megabyte inline.
func (r *repository) File(ctx context.Context, commit, path string, limit int64) ([]byte, error) {
	if !git.ValidObjectID(commit) || path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "..") || limit <= 0 {
		return nil, fmt.Errorf("github: invalid file request")
	}
	client, err := r.client.API(ctx)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	file, _, response, err := client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: commit})
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return nil, githubapi.RateLimitError(err)
	}
	if file == nil || file.GetType() != "file" {
		return nil, fmt.Errorf("%w: %s is not a file at %s", forge.ErrNotFound, path, commit)
	}
	if file.GetSize() > int(limit) {
		return nil, fmt.Errorf("github: %s exceeds %d bytes", path, limit)
	}
	content, err := file.GetContent()
	if err != nil {
		return nil, fmt.Errorf("github: decoding %s: %w", path, err)
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("github: %s exceeds %d bytes", path, limit)
	}
	return []byte(content), nil
}
