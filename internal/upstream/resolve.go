package upstream

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/tcl/syntax"
)

var ErrTagMissing = errors.New("upstream: tag does not exist")
var ErrSourceChanged = errors.New("upstream: selected tag now identifies different source")

type Tag struct{ Name, Commit string }
type TagReader interface {
	Tag(context.Context, string, string) (Tag, error)
}

func RepositoryName(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
				return false
			}
		}
	}
	return true
}

func githubSource(port macports.PortInfo) (string, TagPattern, error) {
	for _, key := range []string{"github.author", "github.project", "github.version", "git.branch"} {
		if port.OptionErrors[key] != "" {
			return "", TagPattern{}, fmt.Errorf("upstream: cannot evaluate %s", key)
		}
	}
	repository := port.Options["github.author"] + "/" + port.Options["github.project"]
	if !RepositoryName(repository) || port.Options["github.version"] != port.Version {
		return "", TagPattern{}, fmt.Errorf("upstream: explicit bumps currently require a GitHub PortGroup version matching the evaluated port version")
	}
	values := []string{}
	for _, key := range []string{"github.tag_prefix", "github.tag_suffix"} {
		value, ok := port.Options[key]
		if !ok || port.OptionErrors[key] != "" {
			return "", TagPattern{}, ErrTagPattern
		}
		parts, errs := syntax.ListValues(value)
		if len(errs) > 0 {
			return "", TagPattern{}, ErrTagPattern
		}
		values = append(values, strings.Join(parts, " "))
	}
	pattern := TagPattern{Prefix: values[0], Suffix: values[1]}
	if port.Options["git.branch"] != pattern.Prefix+port.Version+pattern.Suffix {
		return "", TagPattern{}, ErrTagPattern
	}
	return repository, pattern, nil
}

func (s *Service) Resolve(ctx context.Context, port macports.PortInfo, requested string) (record.Release, error) {
	if err := ValidateVersion(requested); err != nil {
		return record.Release{}, err
	}
	if s == nil || s.Tags == nil {
		return record.Release{}, fmt.Errorf("upstream: tag reader is required")
	}
	repository, pattern, err := githubSource(port)
	if err != nil {
		return record.Release{}, err
	}
	candidates := []string{requested}
	explicit := pattern.Prefix != "" && strings.HasPrefix(requested, pattern.Prefix) || pattern.Suffix != "" && strings.HasSuffix(requested, pattern.Suffix)
	inferred := pattern.Prefix + requested + pattern.Suffix
	if !explicit && inferred != requested {
		candidates = append(candidates, inferred)
	}
	var evidence []Release
	commits := map[string]string{}
	for _, candidate := range candidates {
		tag, err := s.Tags.Tag(ctx, repository, candidate)
		if errors.Is(err, ErrTagMissing) {
			continue
		}
		if err != nil {
			return record.Release{}, err
		}
		if tag.Name != candidate || !git.ValidObjectID(tag.Commit) {
			return record.Release{}, fmt.Errorf("upstream: invalid tag observation")
		}
		evidence = append(evidence, Release{Tag: tag.Name})
		commits[tag.Name] = tag.Commit
	}
	selection, err := MatchRelease(requested, &pattern, evidence)
	if err != nil {
		return record.Release{}, err
	}
	if selection.Release.Version == port.Version {
		return record.Release{}, fmt.Errorf("upstream: %s is already at version %s", port.Name, port.Version)
	}
	return record.Release{Requested: requested, Version: selection.Release.Version, Repository: repository, Tag: selection.Release.Tag, Commit: commits[selection.Release.Tag], ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}, nil
}

func (s *Service) Check(ctx context.Context, port macports.PortInfo, release record.Release) error {
	repository, pattern, err := githubSource(port)
	if err != nil {
		return err
	}
	if repository != release.Repository || release.Tag != pattern.Prefix+release.Version+pattern.Suffix || !git.ValidObjectID(release.Commit) {
		return fmt.Errorf("upstream: resolved release does not match the Portfile source convention")
	}
	if s == nil || s.Tags == nil {
		return fmt.Errorf("upstream: tag reader is required")
	}
	tag, err := s.Tags.Tag(ctx, repository, release.Tag)
	if errors.Is(err, ErrTagMissing) {
		return fmt.Errorf("%w: %s disappeared", ErrSourceChanged, release.Tag)
	}
	if err != nil {
		return err
	}
	if tag.Name != release.Tag || tag.Commit != release.Commit {
		return fmt.Errorf("%w: %s", ErrSourceChanged, release.Tag)
	}
	return nil
}
