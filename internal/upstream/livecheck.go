package upstream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/record"
)

// discoverOverridden runs the maintainer's own livecheck the way Base would
// and proves its answer against the catalog. The livecheck is the
// maintainer's definition of the latest version; the catalog is the forge's
// statement of what exists and how it is fetched. The version the livecheck
// names is mapped to a tag with the port's tag convention, the tag must exist,
// and in releases mode a published release must carry it; the release record
// is then the one the catalog path produces. A version with no tag is a
// maintainer error worth naming, not a silent fallback.
func (s *Service) discoverOverridden(ctx context.Context, port macports.PortInfo, spec portsource.Spec, repository forge.Repository) (result Result, err error) {
	result = Result{CurrentVersion: port.Version, Assessment: Unknown, ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}
	defer func() {
		if err != nil {
			result.Detail = err.Error()
		}
	}()
	// The livecheck names versions in the source's spelling, as Base
	// compares them against livecheck.version.
	current := spec.SourceVersion
	if !automatic(current) {
		return result, fmt.Errorf("%w: require a stable or prerelease numeric version", errAutomaticUnsupported)
	}
	page, _, err := s.listing(ctx, port, spec)
	if err != nil {
		return result, err
	}
	versions, err := s.Versions.ExtractVersions(ctx, spec.Livecheck.Regex, string(page), spec.Livecheck.Multiline)
	if err != nil {
		return result, err
	}
	var candidates []macports.VersionCandidate
	for _, version := range versions {
		if admits(current, version) {
			candidates = append(candidates, macports.VersionCandidate{Version: version, MatchText: version})
		}
	}
	index, comparison, err := s.newest(ctx, current, `^(.*)$`, candidates, "the port's livecheck", "a version")
	if err != nil {
		return result, err
	}
	version := candidates[index].Version
	name := spec.Pattern.Tag(version)
	tag, err := repository.Tag(ctx, name)
	if errors.Is(err, forge.ErrNotFound) {
		return result, fmt.Errorf("%w: the port's livecheck names version %s, but %s has no tag %s", ErrReleaseMissing, version, repository.Name(), name)
	}
	if err != nil {
		return result, err
	}
	if tag.Name != name || !git.ValidObjectID(tag.Commit) {
		return result, fmt.Errorf("upstream: invalid selected tag observation")
	}
	if spec.Catalog == portsource.Releases {
		releases, ok := repository.(forge.ReleaseRepository)
		if !ok {
			return result, fmt.Errorf("upstream: %s catalog does not expose releases", spec.Forge)
		}
		rows, err := releases.Releases(ctx)
		if err != nil {
			return result, err
		}
		published := false
		for _, row := range rows {
			published = published || row.Tag == name && !row.Draft
		}
		if !published {
			return result, fmt.Errorf("%w: the port's livecheck names version %s, but %s has no published release for tag %s", ErrReleaseMissing, version, repository.Name(), name)
		}
	}
	// The release records the evaluated port version; the livecheck's
	// spelling is what the Portfile evaluates it from.
	evaluated := port.Version
	if comparison > 0 {
		evaluated, err = s.EvaluateVersion(ctx, version)
		if err != nil {
			return result, err
		}
		if evaluated == "" || evaluated == port.Version {
			return result, fmt.Errorf("%w: livecheck capture does not change the evaluated version", errAutomaticUnsupported)
		}
	}
	result.ObservedAt = time.Now().UTC().Truncate(time.Millisecond)
	release := record.Release{Selection: record.Selection{CurrentVersion: port.Version, NoUpdate: comparison <= 0}, Version: evaluated, Forge: string(spec.Forge), Instance: spec.Instance, Repository: repository.Name(), Tag: tag.Name, Commit: tag.Commit, ObservedAt: result.ObservedAt}
	result.finish(release, port.Version, "Selected "+tag.Name+" from the port's livecheck", " by the port's livecheck")
	result.Evidence = []Observation{{Source: string(spec.Forge) + "-livecheck", Version: version, URL: spec.Livecheck.URL, ObservedAt: result.ObservedAt}}
	return result, nil
}
