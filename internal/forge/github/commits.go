package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	githubapi "github.com/herbygillot/dockhand/internal/github"
)

var _ forge.DatedRepository = (*repository)(nil)

// CommitTime is when a commit was made, by its committer's date.
func (r *repository) CommitTime(ctx context.Context, commit string) (time.Time, error) {
	if !git.ValidObjectID(commit) {
		return time.Time{}, fmt.Errorf("github: invalid commit")
	}
	client, err := r.client.API(ctx)
	if err != nil {
		return time.Time{}, githubapi.RateLimitError(err)
	}
	owner, repo, _ := strings.Cut(r.name, "/")
	found, response, err := client.Git.GetCommit(ctx, owner, repo, commit)
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return time.Time{}, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return time.Time{}, githubapi.RateLimitError(err)
	}
	if found.GetSHA() != commit || found.GetCommitter().GetDate().IsZero() {
		return time.Time{}, fmt.Errorf("github: commit %s has no date", commit)
	}
	return found.GetCommitter().GetDate().Time, nil
}
