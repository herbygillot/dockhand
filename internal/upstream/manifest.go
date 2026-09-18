package upstream

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/record"
)

// manifestLimit bounds a manifest read from a forge; go.mod is small.
const manifestLimit = 1 << 20

// Manifest reads one file of the port's forge repository at the resolved
// release's commit, for a git-fetched port that downloads no archive to
// read it from. An absent file reports dependency.ErrManifestMissing, as
// the archive read does.
func (s *Service) Manifest(ctx context.Context, port macports.PortInfo, release record.Release, path string) ([]byte, error) {
	if release.Forge == "" || release.Commit == "" {
		return nil, fmt.Errorf("upstream: a forge release with a commit is required to read %s", path)
	}
	spec, repository, err := s.repository(port, false)
	if err != nil {
		return nil, err
	}
	if string(spec.Forge) != release.Forge || spec.Instance != release.Instance || repository.Name() != release.Repository {
		return nil, fmt.Errorf("upstream: the resolved release does not name this port's repository")
	}
	files, ok := repository.(forge.FileRepository)
	if !ok {
		return nil, fmt.Errorf("upstream: %s does not read files", release.Forge)
	}
	data, err := files.File(ctx, release.Commit, path, manifestLimit)
	if errors.Is(err, forge.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s at %s", dependency.ErrManifestMissing, path, release.Commit)
	}
	return data, err
}
