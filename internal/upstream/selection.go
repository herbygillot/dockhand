package upstream

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
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

type Selection struct {
	Requested string
	Release   Release
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
func MatchRelease(requested string, pattern *TagPattern, releases []Release) (Selection, error) {
	if err := ValidateVersion(requested); err != nil {
		return Selection{}, err
	}
	var matches []Selection
	explicitTag := pattern != nil && ((pattern.Prefix != "" && strings.HasPrefix(requested, pattern.Prefix)) || (pattern.Suffix != "" && strings.HasSuffix(requested, pattern.Suffix)))
	for _, release := range releases {
		version := release.Version
		if version == "" && pattern != nil {
			value, prefix := strings.CutPrefix(release.Tag, pattern.Prefix)
			value, suffix := strings.CutSuffix(value, pattern.Suffix)
			if prefix && suffix {
				version = value
			}
		}
		exact := release.Tag == requested || (!explicitTag && release.Version == requested)
		inferred := !explicitTag && pattern != nil && release.Tag == pattern.Prefix+requested+pattern.Suffix
		if !exact && !inferred {
			continue
		}
		release.Version = version
		choice := Selection{Requested: requested, Release: release, Inferred: release.Tag != "" && release.Tag != requested}
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
			tags = append(tags, match.Release.Tag)
		}
		slices.Sort(tags)
		return Selection{}, fmt.Errorf("%w: %q matches %v; specify the exact tag", ErrReleaseAmbiguous, requested, tags)
	}
	selected := matches[0]
	if ValidateVersion(selected.Release.Version) != nil {
		return Selection{}, fmt.Errorf("%w: cannot map tag %q to a Portfile version", ErrTagPattern, selected.Release.Tag)
	}
	if pattern != nil && selected.Release.Tag != "" {
		value, prefix := strings.CutPrefix(selected.Release.Tag, pattern.Prefix)
		value, suffix := strings.CutSuffix(value, pattern.Suffix)
		if prefix && suffix && value != selected.Release.Version {
			return Selection{}, fmt.Errorf("%w: inconsistent version metadata for %q", ErrTagPattern, selected.Release.Tag)
		}
	}
	return selected, nil
}
