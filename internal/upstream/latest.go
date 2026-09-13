package upstream

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

var ErrAutomaticUnsupported = errors.New("upstream: automatic selection does not support this source convention")

// Stable numeric versions include calendar and multi-component versions. Other
// spellings, including prereleases, remain available through explicit selection.
var stableVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*$`)

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
	if s == nil || s.Repositories == nil || s.Versions == nil {
		return result, fmt.Errorf("upstream: repository and version readers are required")
	}
	repository, pattern, err := s.githubSource(port)
	if err != nil {
		return result, err
	}
	mode, err := githubLivecheck(port, repository)
	if err != nil {
		return result, err
	}
	var observations []forge.Release
	if mode == "releases" {
		observations, err = repository.Releases(ctx)
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
		version, matches := pattern.version(release.Tag)
		if !matches || !stableVersion.MatchString(version) {
			continue
		}
		candidates = append(candidates, macports.VersionCandidate{Version: version, URL: repository.TagLivecheckURL(release.Tag)})
		tags = append(tags, release.Tag)
	}
	selection, err := s.Versions.SelectVersion(ctx, port.Version, port.Options["livecheck.regex"], candidates)
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
	result.Release = &record.Release{CurrentVersion: port.Version, Version: result.CandidateVersion, Repository: repository.Name(), Tag: tag.Name, Commit: tag.Commit, ObservedAt: result.ObservedAt, NoUpdate: selection.Comparison <= 0}
	result.Evidence = []Observation{{Source: "github-" + mode, Version: result.CandidateVersion, URL: candidates[index].URL, ObservedAt: result.ObservedAt}}
	if result.Release.NoUpdate {
		result.Assessment = Current
		result.Detail = fmt.Sprintf("Already current at %s; latest eligible version is %s", port.Version, result.CandidateVersion)
	} else {
		result.Assessment = UpdateAvailable
		result.Detail = "Selected " + tag.Name
	}
	return result, nil
}
