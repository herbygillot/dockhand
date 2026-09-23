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

var errAutomaticUnsupported = errors.New("upstream: automatic selection does not support this source convention")

// versionSelector orders and captures versions the way MacPorts does: vercmp
// for order and its native regex for livecheck captures.
type versionSelector interface {
	SelectVersion(context.Context, string, string, []macports.VersionCandidate) (macports.VersionSelection, error)
	ExtractVersions(context.Context, string, string, bool) ([]string, error)
}

func (s *Service) DiscoverPort(ctx context.Context, port macports.PortInfo) (result Result, err error) {
	result = Result{CurrentVersion: port.Version, Assessment: Unknown, ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}
	defer func() {
		if err != nil {
			result.Detail = err.Error()
		}
	}()
	if s == nil || s.Versions == nil || s.EvaluateVersion == nil {
		return result, fmt.Errorf("upstream: version comparison and Portfile evaluation are required")
	}
	discovery, discoveryErr := portsource.Interpret(port, portsource.Discovery)
	if discoveryErr != nil {
		return result, fmt.Errorf("%w: %v", errAutomaticUnsupported, discoveryErr)
	}
	if discovery.Catalog == portsource.HTTPRegex {
		return s.discoverListing(ctx, port, discovery)
	}
	spec, repository, err := s.repository(port, true)
	if err != nil {
		return result, err
	}
	if !automatic(port.Version) {
		return result, fmt.Errorf("%w: require a stable or prerelease numeric version", errAutomaticUnsupported)
	}
	if spec.Livecheck.Overridden {
		return s.discoverOverridden(ctx, port, spec, repository)
	}
	followsPrereleases := followsPrereleases(port.Version)
	var observations []forge.Release
	if spec.Catalog == portsource.Releases {
		releases, ok := repository.(forge.ReleaseRepository)
		if !ok {
			return result, fmt.Errorf("upstream: %s catalog does not expose releases", spec.Forge)
		}
		observations, err = releases.Releases(ctx)
		if err != nil {
			return result, err
		}
	} else {
		tags, err := repository.ListTags(ctx)
		if err != nil {
			return result, err
		}
		for _, tag := range tags {
			observations = append(observations, forge.Release{Tag: tag.Name})
		}
	}
	seen := map[string]bool{}
	for _, observation := range observations {
		if seen[observation.Tag] {
			return result, fmt.Errorf("%w: repeated tag", forge.ErrIncomplete)
		}
		seen[observation.Tag] = true
	}
	var candidates []macports.VersionCandidate
	var tags []string
	for _, release := range observations {
		if release.Draft || release.Prerelease && !followsPrereleases {
			continue
		}
		version, matches := spec.Pattern.Version(release.Tag)
		if !matches || !admits(port.Version, version) {
			continue
		}
		subject, err := spec.MatchText(release.Tag)
		if err != nil {
			return result, err
		}
		candidates = append(candidates, macports.VersionCandidate{Version: version, MatchText: subject, CaptureVersion: version})
		tags = append(tags, release.Tag)
	}
	filters := make([]macports.VersionCandidate, len(candidates))
	copy(filters, candidates)
	for i := range filters {
		filters[i].Version = "1"
	}
	eligible, err := s.Versions.SelectVersion(ctx, "0", spec.Livecheck.Regex, filters)
	if err != nil {
		return result, err
	}
	var evaluated []macports.VersionCandidate
	var selectedTags []string
	var pending []int
	for _, index := range eligible.Indices {
		if index < 0 || index >= len(candidates) {
			return result, fmt.Errorf("upstream: invalid filter result")
		}
		candidate := candidates[index]
		if candidate.CaptureVersion == spec.SourceVersion {
			candidate.Version = port.Version
		} else {
			pending = append(pending, len(evaluated))
		}
		evaluated = append(evaluated, candidate)
		selectedTags = append(selectedTags, tags[index])
	}
	if err := s.evaluateCandidates(ctx, evaluated, selectedTags, pending); err != nil {
		return result, err
	}
	candidates, tags = evaluated, selectedTags
	index, comparison, err := s.newest(ctx, port.Version, spec.Livecheck.Regex, candidates, "the port's livecheck filter", "a tag")
	if err != nil {
		return result, err
	}
	tag, err := repository.Tag(ctx, tags[index])
	if err != nil {
		return result, err
	}
	if tag.Name != tags[index] || !git.ValidObjectID(tag.Commit) {
		return result, fmt.Errorf("upstream: invalid selected tag observation")
	}
	result.ObservedAt = time.Now().UTC().Truncate(time.Millisecond)
	evidenceURL, err := spec.EvidenceURL(tag.Name)
	if err != nil {
		return result, err
	}
	catalog := "tags"
	if spec.Catalog == portsource.Releases {
		catalog = "published releases"
	}
	release := record.Release{Selection: record.Selection{CurrentVersion: port.Version, NoUpdate: comparison <= 0}, Version: candidates[index].Version, Forge: string(spec.Forge), Instance: spec.Instance, Repository: repository.Name(), Tag: tag.Name, Commit: tag.Commit, ObservedAt: result.ObservedAt}
	result.finish(release, port.Version, "Selected "+tag.Name+" from "+catalog, " among "+catalog)
	result.Evidence = []Observation{{Source: string(spec.Forge) + "-" + string(spec.Catalog), Version: release.Version, URL: evidenceURL, ObservedAt: result.ObservedAt}}
	return result, nil
}

// evaluateCandidates fills the evaluated version of every pending candidate,
// in one batch when the bound probe supports it. MacPorts still evaluates each
// candidate; batching only shares the interpreter.
func (s *Service) evaluateCandidates(ctx context.Context, candidates []macports.VersionCandidate, tags []string, pending []int) error {
	if len(pending) == 0 {
		return nil
	}
	if s.EvaluateVersions != nil && len(pending) > 1 {
		values := make([]string, len(pending))
		for i, index := range pending {
			values[i] = candidates[index].CaptureVersion
		}
		versions, err := s.EvaluateVersions(ctx, values)
		if err != nil {
			return fmt.Errorf("upstream: cannot evaluate candidate versions: %w", err)
		}
		if len(versions) != len(values) {
			return fmt.Errorf("upstream: incomplete candidate evaluation")
		}
		for i, index := range pending {
			candidates[index].Version = versions[i]
		}
		return nil
	}
	for _, index := range pending {
		version, err := s.EvaluateVersion(ctx, candidates[index].CaptureVersion)
		if err != nil {
			return fmt.Errorf("upstream: cannot evaluate %s: %w", tags[index], err)
		}
		candidates[index].Version = version
	}
	return nil
}
