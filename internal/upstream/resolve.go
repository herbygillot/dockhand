package upstream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/forge"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

var ErrSourceChanged = errors.New("upstream: selected tag now identifies different source")

func (s *Service) Resolve(ctx context.Context, port macports.PortInfo, requested string) (record.Release, error) {
	if requested == "" {
		result, err := s.DiscoverPort(ctx, port)
		if err != nil {
			return record.Release{}, err
		}
		return *result.Release, nil
	}
	if err := ValidateVersion(requested); err != nil {
		return record.Release{}, err
	}
	spec, repository, err := s.repository(port, false)
	if err != nil {
		return record.Release{}, err
	}
	candidates := []string{requested}
	explicit := spec.Pattern.Explicit(requested)
	inferred := spec.Pattern.Tag(requested)
	if !explicit && inferred != requested {
		candidates = append(candidates, inferred)
	}
	var evidence []Candidate
	commits := map[string]string{}
	for _, candidate := range candidates {
		tag, err := repository.Tag(ctx, candidate)
		if errors.Is(err, forge.ErrNotFound) {
			continue
		}
		if err != nil {
			return record.Release{}, err
		}
		if tag.Name != candidate || !git.ValidObjectID(tag.Commit) {
			return record.Release{}, fmt.Errorf("upstream: invalid tag observation")
		}
		evidence = append(evidence, Candidate{Release: forge.Release{Tag: tag.Name}})
		commits[tag.Name] = tag.Commit
	}
	selection, err := MatchRelease(requested, &spec.Pattern, evidence)
	if err != nil {
		return record.Release{}, err
	}
	if selection.Candidate.Version == port.Version {
		return record.Release{}, fmt.Errorf("upstream: %s is already at version %s", port.Name, port.Version)
	}
	return record.Release{Requested: requested, Version: selection.Candidate.Version, Forge: string(spec.Forge), Instance: spec.Instance, Repository: repository.Name(), Tag: selection.Candidate.Tag, Commit: commits[selection.Candidate.Tag], ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}, nil
}

func (s *Service) Check(ctx context.Context, port macports.PortInfo, release record.Release) error {
	spec, repository, err := s.repository(port, false)
	if err != nil {
		return err
	}
	if string(spec.Forge) != release.Forge || spec.Instance != release.Instance || repository.Name() != release.Repository || release.Tag != spec.Pattern.Tag(release.Version) || !git.ValidObjectID(release.Commit) {
		return fmt.Errorf("upstream: resolved release does not match the Portfile source convention")
	}
	tag, err := repository.Tag(ctx, release.Tag)
	if errors.Is(err, forge.ErrNotFound) {
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
