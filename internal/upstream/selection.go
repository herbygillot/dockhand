package upstream

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/v2/internal/forge"
)

var (
	ErrVersionInput     = errors.New("upstream: invalid explicit version")
	ErrTagPattern       = errors.New("upstream: tag convention is unknown")
	ErrReleaseMissing   = errors.New("upstream: requested release was not found in the supplied evidence")
	ErrReleaseAmbiguous = errors.New("upstream: requested version matches multiple releases")
)

type TagPattern struct {
	Prefix string
	Suffix string
}

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

func ValidateVersion(value string) error {
	if value == "" || !utf8.ValidString(value) || strings.HasPrefix(value, "-") || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return ErrVersionInput
	}
	return nil
}

// PatternFromCurrent requires one identifiable occurrence of the evaluated
// version. Explicit PortGroup prefix/suffix metadata can supply a pattern directly.
func PatternFromCurrent(version, tag string) (TagPattern, error) {
	if ValidateVersion(version) != nil || ValidateVersion(tag) != nil || strings.Count(tag, version) != 1 {
		return TagPattern{}, ErrTagPattern
	}
	prefix, suffix, _ := strings.Cut(tag, version)
	return TagPattern{Prefix: prefix, Suffix: suffix}, nil
}

// MatchRelease judges already collected evidence; lookup failures must be
// handled by the reader, not converted into an empty successful observation.
// A nil pattern means unknown, while an empty pattern means bare version tags.
func MatchRelease(requested string, pattern *TagPattern, releases []Candidate) (Selection, error) {
	if err := ValidateVersion(requested); err != nil {
		return Selection{}, err
	}
	var matches []Selection
	explicitTag := pattern != nil && pattern.explicit(requested)
	for _, release := range releases {
		version := release.Version
		if version == "" && pattern != nil {
			value, matches := pattern.version(release.Tag)
			if matches {
				version = value
			}
		}
		exact := release.Tag == requested || (!explicitTag && release.Version == requested)
		inferred := !explicitTag && pattern != nil && release.Tag == pattern.tag(requested)
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
	if ValidateVersion(selected.Candidate.Version) != nil {
		return Selection{}, fmt.Errorf("%w: cannot map tag %q to a Portfile version", ErrTagPattern, selected.Candidate.Tag)
	}
	if pattern != nil && selected.Candidate.Tag != "" {
		value, matches := pattern.version(selected.Candidate.Tag)
		if matches && value != selected.Candidate.Version {
			return Selection{}, fmt.Errorf("%w: inconsistent version metadata for %q", ErrTagPattern, selected.Candidate.Tag)
		}
	}
	return selected, nil
}

func (p TagPattern) tag(version string) string { return p.Prefix + version + p.Suffix }
func (p TagPattern) version(tag string) (string, bool) {
	version, prefix := strings.CutPrefix(tag, p.Prefix)
	version, suffix := strings.CutSuffix(version, p.Suffix)
	return version, prefix && suffix
}
func (p TagPattern) explicit(value string) bool {
	return p.Prefix != "" && strings.HasPrefix(value, p.Prefix) || p.Suffix != "" && strings.HasSuffix(value, p.Suffix)
}
