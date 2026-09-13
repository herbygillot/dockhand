package upstream

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

var ErrIncomplete = errors.New("upstream: incomplete release evidence")
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
	if s == nil || s.Releases == nil || s.Tags == nil || s.Versions == nil {
		return result, fmt.Errorf("upstream: release, tag, and version readers are required")
	}
	repository, pattern, err := githubSource(port)
	if err != nil {
		return result, err
	}
	for _, key := range []string{"github.tarball_from", "livecheck.type", "livecheck.url", "livecheck.regex", "livecheck.version"} {
		if port.OptionErrors[key] != "" {
			return result, fmt.Errorf("%w: cannot evaluate %s", ErrAutomaticUnsupported, key)
		}
	}
	base := "https://github.com/" + repository
	if port.Options["livecheck.type"] != "regex" || strings.TrimRight(port.Options["livecheck.url"], "/") != base+"/tags" || port.Options["livecheck.regex"] == "" || port.Options["livecheck.version"] != port.Version || !stableVersion.MatchString(port.Version) {
		return result, fmt.Errorf("%w: require a stable numeric version and a matching GitHub tags livecheck", ErrAutomaticUnsupported)
	}
	mode := port.Options["github.tarball_from"]
	if mode != "releases" && mode != "archive" && mode != "tarball" {
		return result, fmt.Errorf("%w: unknown GitHub archive mode", ErrAutomaticUnsupported)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	releases, err := s.Releases.Releases(ctx, repository)
	if err != nil {
		return result, err
	}
	known := map[string]Release{}
	for _, release := range releases {
		if _, exists := known[release.Tag]; exists {
			return result, fmt.Errorf("%w: repeated release tag", ErrIncomplete)
		}
		known[release.Tag] = release
	}
	var observations []Release
	if mode == "releases" {
		observations = releases
	} else {
		tags, err := s.Releases.ListTags(ctx, repository)
		if err != nil {
			return result, err
		}
		seen := map[string]bool{}
		for _, tag := range tags {
			if seen[tag.Name] {
				return result, fmt.Errorf("%w: repeated tag", ErrIncomplete)
			}
			seen[tag.Name] = true
			release, exists := known[tag.Name]
			if !exists {
				release = Release{Tag: tag.Name}
			}
			observations = append(observations, release)
		}
	}
	var candidates []macports.VersionCandidate
	var tags []string
	for _, release := range observations {
		if release.Draft || release.Prerelease {
			continue
		}
		version, prefix := strings.CutPrefix(release.Tag, pattern.Prefix)
		version, suffix := strings.CutSuffix(version, pattern.Suffix)
		if !prefix || !suffix || !stableVersion.MatchString(version) {
			continue
		}
		candidates = append(candidates, macports.VersionCandidate{Version: version, URL: base + "/archive/refs/tags/" + release.Tag + ".tar.gz"})
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
	tag, err := s.Tags.Tag(ctx, repository, tags[index])
	if err != nil {
		return result, err
	}
	if tag.Name != tags[index] || !git.ValidObjectID(tag.Commit) {
		return result, fmt.Errorf("upstream: invalid selected tag observation")
	}
	result.ObservedAt = time.Now().UTC().Truncate(time.Millisecond)
	result.CandidateVersion = candidates[index].Version
	result.Release = &record.Release{CurrentVersion: port.Version, Version: result.CandidateVersion, Repository: repository, Tag: tag.Name, Commit: tag.Commit, ObservedAt: result.ObservedAt, NoUpdate: selection.Comparison <= 0}
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
