package upstream

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/record"
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
	if err := version.Validate(requested); err != nil {
		return record.Release{}, err
	}
	editable, err := portsource.Interpret(port, portsource.Edit)
	if err != nil {
		return record.Release{}, err
	}
	if editable.Forge == "" {
		if s.EvaluateVersion == nil {
			return record.Release{}, fmt.Errorf("upstream: Portfile version evaluation is required")
		}
		// The request is the source's spelling; the port version is what
		// the Portfile evaluates from it, the same string for most ports.
		version, err := s.EvaluateVersion(ctx, requested)
		if err != nil {
			return record.Release{}, err
		}
		if version == "" || version == port.Version {
			return record.Release{}, fmt.Errorf("upstream: explicit archive version must select a different evaluated version")
		}
		release := record.Release{Selection: record.Selection{Requested: requested, CurrentVersion: port.Version}, Archive: true, Version: version, ObservedAt: time.Now().UTC()}
		if version != requested {
			release.SourceVersion = requested
		}
		return classified(release, port.Version), nil
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
	version := selection.Candidate.Version
	if s.EvaluateVersion == nil {
		return record.Release{}, fmt.Errorf("upstream: Portfile version evaluation is required")
	}
	version, err = s.EvaluateVersion(ctx, version)
	if err != nil {
		return record.Release{}, err
	}

	if version == port.Version {
		return record.Release{}, fmt.Errorf("upstream: %s is already at version %s", port.Name, port.Version)
	}
	return classified(record.Release{Selection: record.Selection{Requested: requested, CurrentVersion: port.Version}, Version: version, Forge: string(spec.Forge), Instance: spec.Instance, Repository: repository.Name(), Tag: selection.Candidate.Tag, Commit: commits[selection.Candidate.Tag], ObservedAt: time.Now().UTC().Truncate(time.Millisecond)}, port.Version), nil
}

func (s *Service) Check(ctx context.Context, port macports.PortInfo, release record.Release) error {
	if release.Archive {
		spec, err := portsource.Interpret(port, portsource.Edit)
		if err != nil {
			return err
		}
		// A frozen archive release must describe this Portfile's source; an
		// already-current one is coherent too and lets preparation report
		// "no update" the way it does for a forge source.
		if spec.Forge != "" || release.Requested != "" && release.SourceSpelling() != release.Requested || release.CurrentVersion != port.Version || release.Forge != "" || release.Instance != "" || release.Repository != "" || release.Tag != "" || release.Commit != "" || version.Validate(release.Version) != nil || release.SourceVersion != "" && version.Validate(release.SourceVersion) != nil {
			return fmt.Errorf("upstream: archive release does not match the Portfile")
		}
		if release.Requested == "" {
			observed, err := portsource.Interpret(port, portsource.Discovery)
			if err != nil {
				return err
			}
			if release.Listing == nil || release.Listing.URL != observed.Livecheck.URL || release.Listing.SHA256 == "" {
				return fmt.Errorf("upstream: archive discovery evidence does not match the Portfile")
			}
		}
		return nil
	}
	spec, repository, err := s.repository(port, false)
	if err != nil {
		return err
	}
	if string(spec.Forge) != release.Forge || spec.Instance != release.Instance || repository.Name() != release.Repository || !matchesTag(spec, release.Tag) || !git.ValidObjectID(release.Commit) {
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

func matchesTag(spec portsource.Spec, tag string) bool {
	_, ok := spec.Pattern.Version(tag)
	return ok
}
