package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	sdk "gitlab.com/gitlab-org/api/client-go/v2"
)

// File reads one file at a commit through the raw file endpoint, bounded
// by limit bytes.
func (r *repository) File(ctx context.Context, commit, path string, limit int64) ([]byte, error) {
	if !git.ValidObjectID(commit) || path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "..") || limit <= 0 {
		return nil, fmt.Errorf("gitlab: invalid file request")
	}
	client, err := r.api()
	if err != nil {
		return nil, err
	}
	body, response, err := client.RepositoryFiles.GetRawFile(r.project, path, &sdk.GetRawFileOptions{Ref: sdk.Ptr(commit)}, sdk.WithContext(ctx))
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return nil, fmt.Errorf("gitlab: reading %s: %w", path, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("gitlab: %s exceeds %d bytes", path, limit)
	}
	return body, nil
}
