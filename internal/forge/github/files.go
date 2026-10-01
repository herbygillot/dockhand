package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// The contents API's media types, as GitHub documents them
// (https://docs.github.com/en/rest/repos/contents#get-repository-content):
// the object type says what a path is, a file's size, and a file's
// content up to 1 MB, inline; past that its content is empty, its encoding
// "none", and the raw type serves the file itself, up to 100 MB.
const (
	contentsObject = "application/vnd.github.object+json"
	contentsRaw    = "application/vnd.github.raw+json"
)

// File reads one file at a commit through the contents API: what the path
// is first, and the file's content inline where GitHub gives it, else the
// raw file. Read inline alone, a file past 1 MB failed with "unsupported
// content encoding: none", short of the caller's limit, as create's 4 MiB
// of a build file (the limits sweep, 2026-10-01); the limit is what
// applies now.
func (r *repository) File(ctx context.Context, commit, path string, limit int64) ([]byte, error) {
	if !git.ValidObjectID(commit) || path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "..") || limit <= 0 {
		return nil, fmt.Errorf("github: invalid file request")
	}
	client, err := r.client.API(ctx)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	address := fmt.Sprintf("repos/%s/%s/contents/%s?ref=%s", owner, repo, (&url.URL{Path: path}).String(), url.QueryEscape(commit))
	request, err := client.NewRequest(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", contentsObject)
	var file *github.RepositoryContent
	response, err := client.Do(request, &file)
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return nil, githubapi.RateLimitError(err)
	}
	if file == nil || file.GetType() != "file" {
		return nil, fmt.Errorf("%w: %s is not a file at %s", forge.ErrNotFound, path, commit)
	}
	tooLarge := fmt.Errorf("github: %s is larger than the %d KiB dockhand reads of it", path, limit>>10)
	if int64(file.GetSize()) > limit {
		return nil, tooLarge
	}
	if file.GetEncoding() != "none" {
		content, err := file.GetContent()
		if err != nil {
			return nil, fmt.Errorf("github: decoding %s: %w", path, err)
		}
		if int64(len(content)) > limit {
			return nil, tooLarge
		}
		return []byte(content), nil
	}
	request, err = client.NewRequest(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", contentsRaw)
	raw, err := client.BareDo(request)
	if err != nil {
		return nil, githubapi.RateLimitError(err)
	}
	defer raw.Body.Close()
	data, err := io.ReadAll(io.LimitReader(raw.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("github: reading %s: %w", path, err)
	}
	if int64(len(data)) > limit {
		return nil, tooLarge
	}
	return data, nil
}
