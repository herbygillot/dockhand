package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	sdk "gitlab.com/gitlab-org/api/client-go/v2"
)

var _ forge.DatedRepository = (*repository)(nil)

// CommitTime is when a commit was made, by its committer's date.
func (r *repository) CommitTime(ctx context.Context, commit string) (time.Time, error) {
	if !git.ValidObjectID(commit) {
		return time.Time{}, fmt.Errorf("gitlab: invalid commit")
	}
	client, err := r.api()
	if err != nil {
		return time.Time{}, err
	}
	found, response, err := client.Commits.GetCommit(r.project, commit, nil, sdk.WithContext(ctx))
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return time.Time{}, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return time.Time{}, fmt.Errorf("gitlab: reading commit %s: %w", commit, err)
	}
	if found == nil || found.ID != commit || found.CommittedDate == nil {
		return time.Time{}, fmt.Errorf("gitlab: commit %s has no date", commit)
	}
	return *found.CommittedDate, nil
}
