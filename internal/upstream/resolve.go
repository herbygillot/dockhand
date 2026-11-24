package upstream

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/macports/version"
	"github.com/herbygillot/dockhand/internal/model"
)

var ErrSourceChanged = errors.New("upstream: selected tag now identifies different source")

func (s *Service) Resolve(ctx context.Context, port macports.PortInfo, requested string) (release model.Release, err error) {
	err = s.withVersionSession(ctx, func(s *Service) error {
		release, err = s.resolve(ctx, port, requested)
		return err
	})
	return release, err
}

func (s *Service) resolve(ctx context.Context, port macports.PortInfo, requested string) (model.Release, error) {
	// An obsolete stub follows the port that replaced it, whose update
	// moves it where it moves at all (update --with-obsolete).
	var stub *ReplacedError
	if errors.As(replaced(port), &stub) {
		return model.Release{}, fmt.Errorf("upstream: %s is an obsolete stub, %s, so it has no release of its own; dockhand update %s updates the port that replaced it", stub.Port, stub, stub.By)
	}
	if spec, ok := pinned(port); ok {
		return s.resolvePin(ctx, port, spec, requested)
	}
	if requested == "" {
		result, err := s.DiscoverPort(ctx, port)
		if err != nil {
			return model.Release{}, err
		}
		// A port discovery can't call current or outdated has no release
		// to choose: the update needs one named.
		if result.Assessment == Uncertain {
			return model.Release{}, &UncertainError{Port: port.Name, SetAside: result.SetAside}
		}
		if result.Assessment == Ahead {
			return model.Release{}, &AheadError{Port: port.Name, Current: port.Version, Tag: result.NewestTag}
		}
		if result.Assessment == OwnVersion {
			return model.Release{}, fmt.Errorf("%w: %s fetches no source on this platform, and livecheck does not use its version; specify a version with dockhand update %s <version>", ErrOwnVersion, port.Name, port.Name)
		}
		// A branch's head, as one a port tracks names, is no release.
		if result.Release == nil {
			return model.Release{}, fmt.Errorf("upstream: %s has no release to move to; specify a version with dockhand update %s <version>", port.Name, port.Name)
		}
		return *result.Release, nil
	}
	if err := version.Validate(requested); err != nil {
		return model.Release{}, err
	}
	editable, err := portsource.Interpret(port, portsource.Edit)
	if err != nil {
		return model.Release{}, err
	}
	if editable.Forge == "" {
		if s.EvaluateVersion == nil {
			return model.Release{}, fmt.Errorf("upstream: Portfile version evaluation is required")
		}
		// The version the port is at is current (#165).
		if requested == port.Version || editable.SourceVersion != "" && requested == editable.SourceVersion {
			release := model.Release{ReleaseSelection: model.ReleaseSelection{Requested: requested, CurrentVersion: port.Version, NoUpdate: true}, Archive: true, Version: port.Version}
			if requested != port.Version {
				release.SourceVersion = requested
			}
			return classified(release, port.Version), nil
		}
		// The request is the source's spelling; the port version is what
		// the Portfile evaluates from it, the same string for most ports.
		version, err := s.EvaluateVersion(ctx, requested)
		if err != nil {
			return model.Release{}, err
		}
		if version == "" || version == port.Version {
			return model.Release{}, fmt.Errorf("upstream: explicit archive version must select a different evaluated version")
		}
		release := model.Release{ReleaseSelection: model.ReleaseSelection{Requested: requested, CurrentVersion: port.Version}, Archive: true, Version: version}
		if version != requested {
			release.SourceVersion = requested
		}
		return s.ordered(ctx, classified(release, port.Version))
	}
	spec, repository, err := s.repository(port, false)
	if err != nil {
		return model.Release{}, err
	}
	candidates := []string{requested}
	explicit := spec.Pattern.Explicit(requested)
	inferred := spec.Pattern.Tag(requested)
	if !explicit && inferred != requested {
		candidates = append(candidates, inferred)
	}
	// A port whose version is its source's with other separators, as
	// libunibreak's 7.0 is the tag's 7_0, takes the version as people
	// type it, 8.0, as the tag spells it, 8_0 (field testing, batch 12).
	spelled := sourceSpelling(requested, port.Version, spec.SourceVersion)
	if !explicit && spelled != requested {
		candidates = append(candidates, spelled, spec.Pattern.Tag(spelled))
	}
	var evidence []Candidate
	commits := map[string]string{}
	for _, candidate := range candidates {
		tag, err := repository.Tag(ctx, candidate)
		if errors.Is(err, forge.ErrNotFound) {
			continue
		}
		if err != nil {
			return model.Release{}, err
		}
		if tag.Name != candidate || !git.ValidObjectID(tag.Commit) {
			return model.Release{}, fmt.Errorf("upstream: invalid tag observation")
		}
		evidence = append(evidence, Candidate{Release: forge.Release{Tag: tag.Name}})
		commits[tag.Name] = tag.Commit
	}
	selection, err := MatchRelease(requested, &spec.Pattern, evidence)
	if err != nil && spelled != requested {
		selection, err = MatchRelease(spelled, &spec.Pattern, evidence)
	}
	// A version in the port's form, which a Portfile maps from the tag in
	// a way no spelling above undoes, as rust-analyzer's 20261123 from
	// 2026-11-23: each release's version is read for the port, as
	// discovery reads it, and the one the request names is taken (#305).
	if missing := err; errors.Is(missing, ErrReleaseMissing) && !explicit {
		if selection, err = s.byPortVersion(ctx, port, spec, repository, requested, commits); err != nil && !errors.Is(err, ErrReleaseMissing) && !errors.Is(err, ErrReleaseAmbiguous) {
			// The releases couldn't be read as the port's versions: the
			// request is missing as far as dockhand could tell, and why.
			err = fmt.Errorf("%w; reading %s's releases as the port's versions: %w", missing, repository.Name(), err)
		}
	}
	if err != nil {
		return model.Release{}, err
	}
	version := selection.Candidate.Version
	if s.EvaluateVersion == nil {
		return model.Release{}, fmt.Errorf("upstream: Portfile version evaluation is required")
	}
	// The release the Portfile is at already is the current one, which the
	// update says it is, as it does with no version named: evaluating it
	// found no version input to set, since the literal holds it, and update
	// nvtop 3.4.0 on 3.4.0 refused as an edit it couldn't make and started
	// an empty branch (#165).
	if version == port.Version || spec.SourceVersion != "" && version == spec.SourceVersion {
		version = port.Version
	} else if version, err = s.EvaluateVersion(ctx, version); err != nil {
		return model.Release{}, err
	}
	release := model.Release{ReleaseSelection: model.ReleaseSelection{Requested: requested, CurrentVersion: port.Version}, Version: version, Forge: string(spec.Forge), Instance: spec.Instance, Repository: repository.Name(), Tag: selection.Candidate.Tag, Commit: commits[selection.Candidate.Tag]}
	if version == port.Version {
		release.NoUpdate = true
		return classified(release, port.Version), nil
	}
	return s.ordered(ctx, classified(release, port.Version))
}

// ordered marks a version the person named that vercmp orders below the
// current one (Lowers), the docs dig's I3: `update jq 1.6` on 1.7 wrote a
// version `port upgrade` never takes, stranding every install, and said
// nothing. Automatic selection never chooses one (Ahead).
func (s *Service) ordered(ctx context.Context, release model.Release) (model.Release, error) {
	if release.CurrentVersion == "" || s.Versions == nil {
		return release, nil
	}
	order, err := s.Versions.CompareVersions(ctx, release.Version, release.CurrentVersion)
	if err != nil {
		return model.Release{}, err
	}
	release.Lowers = order < 0
	return release, nil
}

func (s *Service) Check(ctx context.Context, port macports.PortInfo, release model.Release) error {
	if release.Pinned {
		return s.checkPin(ctx, port, release)
	}
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

// sourceSpelling is a requested version in the source's spelling, where
// the port's version is the source's with its separators changed, 7.0
// for 7_0: 8.0 is asked for as 8_0. Otherwise it's the version asked.
func sourceSpelling(requested, current, source string) string {
	separators := func(r rune) rune {
		if r == '.' || r == '_' || r == '-' {
			return '.'
		}
		return r
	}
	if source == "" || source == current || strings.Map(separators, source) != strings.Map(separators, current) {
		return requested
	}
	separator := rune(0)
	for _, r := range source {
		if r == '.' || r == '_' || r == '-' {
			separator = r
			break
		}
	}
	if separator == 0 {
		return requested
	}
	return strings.Map(func(r rune) rune {
		if r == '.' || r == '_' || r == '-' {
			return separator
		}
		return r
	}, requested)
}

// byPortVersion selects the release whose version, as the Portfile reads it
// from the release's tag, is requested: the catalog's releases, each read
// as discovery reads it (evaluateCandidates), the one reader of which
// version a release is for the port. Where none is, the refusal names the
// nearest releases in both forms, upstream's tag and the port's version,
// where they differ. The selected tag's commit goes in commits.
func (s *Service) byPortVersion(ctx context.Context, port macports.PortInfo, spec portsource.Spec, repository forge.Repository, requested string, commits map[string]string) (Selection, error) {
	observations, err := catalogReleases(ctx, spec, repository)
	if err != nil {
		return Selection{}, err
	}
	var candidates []macports.VersionCandidate
	var tags []string
	var pending []int
	for _, release := range observations {
		capture, matches := spec.Pattern.Version(release.Tag)
		if release.Draft || !matches {
			continue
		}
		candidate := macports.VersionCandidate{CaptureVersion: capture}
		if capture == spec.SourceVersion {
			candidate.Version = port.Version
		} else {
			pending = append(pending, len(candidates))
		}
		candidates = append(candidates, candidate)
		tags = append(tags, release.Tag)
	}
	if err := s.evaluateCandidates(ctx, candidates, tags, pending); err != nil {
		return Selection{}, err
	}
	var found []int
	for index, candidate := range candidates {
		if candidate.Version == requested {
			found = append(found, index)
		}
	}
	switch len(found) {
	case 0:
		return Selection{}, fmt.Errorf("%w: %q%s", ErrReleaseMissing, requested, nearestWords(requested, candidates, tags))
	case 1:
	default:
		matched := make([]string, 0, len(found))
		for _, index := range found {
			matched = append(matched, tags[index])
		}
		slices.Sort(matched)
		return Selection{}, fmt.Errorf("%w: %q matches %v; specify the exact tag", ErrReleaseAmbiguous, requested, matched)
	}
	index := found[0]
	tag, err := repository.Tag(ctx, tags[index])
	if err != nil {
		return Selection{}, err
	}
	if tag.Name != tags[index] || !git.ValidObjectID(tag.Commit) {
		return Selection{}, fmt.Errorf("upstream: invalid tag observation")
	}
	commits[tag.Name] = tag.Commit
	return Selection{Candidate: Candidate{Version: candidates[index].CaptureVersion, Release: forge.Release{Tag: tag.Name}}}, nil
}

// nearestReleases is how many releases a refusal names.
const nearestReleases = 3

// nearestWords names the releases whose versions, as the port reads them,
// share the longest start with requested, the newest-sorting tag first
// among equals: each by its tag, and its version where that differs.
func nearestWords(requested string, candidates []macports.VersionCandidate, tags []string) string {
	order := make([]int, len(candidates))
	for i := range order {
		order[i] = i
	}
	shared := func(version string) int {
		n := 0
		for n < len(version) && n < len(requested) && version[n] == requested[n] {
			n++
		}
		return n
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if d := shared(candidates[b].Version) - shared(candidates[a].Version); d != 0 {
			return d
		}
		return strings.Compare(tags[b], tags[a])
	})
	var words []string
	for _, index := range order[:min(len(order), nearestReleases)] {
		word := "tag " + tags[index]
		if candidates[index].Version != tags[index] {
			word += ", version " + candidates[index].Version
		}
		words = append(words, word)
	}
	if len(words) == 0 {
		return ""
	}
	return "; the nearest releases are " + strings.Join(words, "; ")
}
