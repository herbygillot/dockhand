package upstream

import (
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"slices"

	"github.com/herbygillot/dockhand/internal/forge"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
)

var (
	ErrTagPattern       = portsource.ErrTagPattern
	ErrReleaseMissing   = errors.New("upstream: requested release was not found in the supplied evidence")
	ErrReleaseAmbiguous = errors.New("upstream: requested version matches multiple releases")
)

type TagPattern = version.TagPattern

// Candidate pairs a possible Portfile version with the observed release it describes.
type Candidate struct {
	Version string
	forge.Release
}

type Selection struct {
	Requested string
	Candidate Candidate
	Inferred  bool
}

// MatchRelease judges already collected evidence; lookup failures must be
// handled by the reader, not converted into an empty successful observation.
// A nil pattern means unknown, while an empty pattern means bare version tags.
func MatchRelease(requested string, pattern *TagPattern, releases []Candidate) (Selection, error) {
	if err := version.Validate(requested); err != nil {
		return Selection{}, err
	}
	var matches []Selection
	explicitTag := pattern != nil && pattern.Explicit(requested)
	for _, release := range releases {
		version := release.Version
		if version == "" && pattern != nil {
			value, matches := pattern.Version(release.Tag)
			if matches {
				version = value
			}
		}
		exact := release.Tag == requested || (!explicitTag && release.Version == requested)
		inferred := !explicitTag && pattern != nil && release.Tag == pattern.Tag(requested)
		if !exact && !inferred {
			continue
		}
		release.Version = version
		choice := Selection{Requested: requested, Candidate: release, Inferred: release.Tag != "" && release.Tag != requested}
		if !slices.Contains(matches, choice) {
			matches = append(matches, choice)
		}
	}
	if len(matches) == 0 {
		return Selection{}, fmt.Errorf("%w: %q", ErrReleaseMissing, requested)
	}
	if len(matches) != 1 {
		tags := make([]string, 0, len(matches))
		for _, match := range matches {
			tags = append(tags, match.Candidate.Tag)
		}
		slices.Sort(tags)
		return Selection{}, fmt.Errorf("%w: %q matches %v; specify the exact tag", ErrReleaseAmbiguous, requested, tags)
	}
	selected := matches[0]
	if version.Validate(selected.Candidate.Version) != nil {
		return Selection{}, fmt.Errorf("%w: cannot map tag %q to a Portfile version", ErrTagPattern, selected.Candidate.Tag)
	}
	if pattern != nil && selected.Candidate.Tag != "" {
		value, matches := pattern.Version(selected.Candidate.Tag)
		if matches && value != selected.Candidate.Version {
			return Selection{}, fmt.Errorf("%w: inconsistent version metadata for %q", ErrTagPattern, selected.Candidate.Tag)
		}
	}
	return selected, nil
}
