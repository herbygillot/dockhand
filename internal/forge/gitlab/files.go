package gitlab

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	sdk "gitlab.com/gitlab-org/api/client-go/v2"
)

// File reads one file at a commit through the raw file endpoint, bounded
// by limit bytes as it reads: the whole body was read before the limit
// was checked, however large (the limits sweep, 2026-10-01).
func (r *repository) File(ctx context.Context, commit, path string, limit int64) ([]byte, error) {
	if !git.ValidObjectID(commit) || path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "..") || limit <= 0 {
		return nil, fmt.Errorf("gitlab: invalid file request")
	}
	client, err := r.api()
	if err != nil {
		return nil, err
	}
	body, response, err := client.RepositoryFiles.GetRawFileReader(r.project, path, &sdk.GetRawFileOptions{Ref: sdk.Ptr(commit)}, sdk.WithContext(ctx))
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %w", forge.ErrNotFound, err)
		}
		return nil, fmt.Errorf("gitlab: reading %s: %w", path, err)
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("gitlab: reading %s: %w", path, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("gitlab: %s is larger than the %d KiB dockhand reads of it", path, limit>>10)
	}
	return data, nil
}
