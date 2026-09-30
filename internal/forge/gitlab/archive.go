package gitlab

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	sdk "gitlab.com/gitlab-org/api/client-go/v2"
)

// Archive writes the tarball GitLab makes of the repository's files at a
// commit, through its documented archive endpoint ("Get file archive"),
// bounded by limit bytes. It's the commit's files as GitLab archives them,
// submodules left out, not what a clone checks out.
func (r *repository) Archive(ctx context.Context, commit string, into io.Writer, limit int64) error {
	if !git.ValidObjectID(commit) || limit <= 0 {
		return fmt.Errorf("gitlab: invalid archive request")
	}
	client, err := r.api()
	if err != nil {
		return err
	}
	bounded := &boundedWriter{into: into, remaining: limit}
	response, err := client.Repositories.StreamArchive(r.project, bounded, &sdk.ArchiveOptions{Format: sdk.Ptr("tar.gz"), SHA: sdk.Ptr(commit)}, sdk.WithContext(ctx))
	switch {
	case errors.Is(err, errTooLarge):
		return fmt.Errorf("gitlab: the archive of %s exceeds %d bytes", commit, limit)
	case err != nil && response != nil && response.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %w", forge.ErrNotFound, err)
	case err != nil:
		return fmt.Errorf("gitlab: fetching the archive of %s: %w", commit, err)
	}
	return nil
}

var errTooLarge = errors.New("too large")

// boundedWriter writes at most remaining bytes more.
type boundedWriter struct {
	into      io.Writer
	remaining int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errTooLarge
	}
	w.remaining -= int64(len(p))
	return w.into.Write(p)
}
