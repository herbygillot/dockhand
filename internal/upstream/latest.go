package upstream

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/record"
)

var ErrAutomaticUnsupported = errors.New("upstream: automatic selection does not support this source convention")

// Stable numeric versions include calendar and multi-component versions. Other
// spellings, including prereleases, remain available through explicit selection.
var stableVersion = regexp.MustCompile(`^[0-9]+(?:[.-][0-9]+)*$`)

type VersionSelector interface {
	SelectVersion(context.Context, string, string, []macports.VersionCandidate) (macports.VersionSelection, error)
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
	discovery, discoveryErr := portsource.Discover(port)
	if discoveryErr != nil {
		return result, fmt.Errorf("%w: %v", ErrAutomaticUnsupported, discoveryErr)
	}
	if discovery.Catalog == portsource.HTTPRegex {
		return s.discoverListing(ctx, port, discovery)
	}
	spec, repository, err := s.repository(port, true)
	if err != nil {
		return result, err
	}
	if !stableVersion.MatchString(port.Version) {
		return result, fmt.Errorf("%w: require a stable numeric version", ErrAutomaticUnsupported)
	}
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
		if release.Draft || release.Prerelease {
			continue
		}
		version, matches := spec.Pattern.Version(release.Tag)
		if !matches || !stableVersion.MatchString(version) {
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
	selection, err := s.Versions.SelectVersion(ctx, port.Version, spec.Livecheck.Regex, candidates)
	if err != nil {
		return result, err
	}
	if len(selection.Indices) == 0 {
		return result, fmt.Errorf("%w: no eligible stable version matches the port's livecheck filter", ErrReleaseMissing)
	}
	if len(selection.Indices) != 1 {
		return result, fmt.Errorf("%w: multiple tags compare equal as the newest version; specify a tag explicitly", ErrReleaseAmbiguous)
	}
	index := selection.Indices[0]
	if index < 0 || index >= len(candidates) || selection.Comparison < -1 || selection.Comparison > 1 {
		return result, fmt.Errorf("upstream: invalid version selection")
	}
	tag, err := repository.Tag(ctx, tags[index])
	if err != nil {
		return result, err
	}
	if tag.Name != tags[index] || !git.ValidObjectID(tag.Commit) {
		return result, fmt.Errorf("upstream: invalid selected tag observation")
	}
	result.ObservedAt = time.Now().UTC().Truncate(time.Millisecond)
	result.CandidateVersion = candidates[index].Version
	result.Release = &record.Release{CurrentVersion: port.Version, Version: result.CandidateVersion, Forge: string(spec.Forge), Instance: spec.Instance, Repository: repository.Name(), Tag: tag.Name, Commit: tag.Commit, ObservedAt: result.ObservedAt, NoUpdate: selection.Comparison <= 0}
	evidenceURL, err := spec.EvidenceURL(tag.Name)
	if err != nil {
		return result, err
	}
	result.Evidence = []Observation{{Source: string(spec.Forge) + "-" + string(spec.Catalog), Version: result.CandidateVersion, URL: evidenceURL, ObservedAt: result.ObservedAt}}
	catalog := "tags"
	if spec.Catalog == portsource.Releases {
		catalog = "published releases"
	}
	if result.Release.NoUpdate {
		result.Assessment = Current
		result.Detail = fmt.Sprintf("Already current at %s; latest eligible version among %s is %s", port.Version, catalog, result.CandidateVersion)
	} else {
		result.Assessment = UpdateAvailable
		result.Detail = "Selected " + tag.Name + " from " + catalog
	}
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
