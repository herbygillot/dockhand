package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/go-github/v91/github"
	"github.com/herbygillot/dockhand/internal/fetch"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

// Archive writes the tarball GitHub makes of the repository's files at a
// commit, through its documented archive link ("Download a repository
// archive (tar)"), which redirects to where the tarball is served, fetched
// as fetch fetches, bounded by limit bytes. It's the commit's files as
// GitHub archives them, submodules left out, not what a clone checks out.
func (r *repository) Archive(ctx context.Context, commit string, into io.Writer, limit int64) error {
	if !git.ValidObjectID(commit) || limit <= 0 {
		return fmt.Errorf("github: invalid archive request")
	}
	client, err := r.client.API(ctx)
	if err != nil {
		return githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	link, response, err := client.Repositories.GetArchiveLink(ctx, owner, repo, github.Tarball, &github.RepositoryContentGetOptions{Ref: commit}, 1)
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return githubapi.RateLimitError(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", fetch.UserAgent)
	served, err := fetch.Open(http.DefaultClient, request, limit)
	if err != nil {
		return fmt.Errorf("github: fetching the archive of %s: %w", commit, err)
	}
	defer served.Body.Close()
	_, err = io.Copy(into, served.Body)
	return err
}
