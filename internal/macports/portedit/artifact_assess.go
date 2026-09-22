package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"maps"
)

func (s *Service) assessArchives(ctx context.Context, request Request, input *sourceInput) (coverage []ContextCoverage, fetchErr, checksumErr error) {
	if gitFetched(input.info) {
		return []ContextCoverage{{Fetch: input.info.Fetch, Platform: input.before.Platform}}, checkGitSource(input.info), nil
	}
	profiles, err := input.observe.Profiles(ctx, input.data)
	if err != nil {
		return coverage, err, nil
	}
	declared, covered, inert := map[string]bool{}, map[string]bool{}, map[string]bool{}
	// Only the selected port is read below, so each context evaluates it
	// alone; the family is for edits, whose fidelity must see siblings.
	observations, err := input.observe.Observe(ctx, input.data, profiles, true, true)
	if err != nil {
		return coverage, err, nil
	}
	for i, profile := range profiles {
		observed := observations[i]
		info := observed.Snapshot.Ports[input.target.Name]
		coverage = append(coverage, ContextCoverage{Platform: profile, Modeled: observed.Modeled, Fetch: info.Fetch})
		if obsoleteIn(info) {
			// An obsolete follower has no archive in this context by design.
			continue
		}
		if err := archives.CheckPolicy(info, input.portdirIn(observed.Snapshot.Root)); err != nil {
			return coverage, err, nil
		}
		metadata, inconclusive := observe.Tolerate(ctx, observed.Ports[input.target.Name], input.data, observed.Snapshot.Root)
		if inconclusive {
			return coverage, fmt.Errorf("%w: modeled context depends on host state%s", errProbeInconclusive, observe.HostInputs(metadata)), nil
		}
		if len(metadata.Problems) > 0 {
			return coverage, fmt.Errorf("%w: %v", ErrUnsupported, metadata.Problems), nil
		}
		if len(metadata.Distfiles) == 0 {
			return coverage, fmt.Errorf("%w: no source archives; select a release subport if this is a metaport", ErrUnsupported), nil
		}
		binding, err := distfiles.Bind(input.data, input.portfileIn(observed.Snapshot.Root), info, metadata)
		if err != nil {
			return coverage, nil, err
		}
		for _, group := range binding.Groups {
			declared[group.ID()] = true
		}
		maps.Copy(inert, inertChecksumGroups(info, binding.Groups))
		for _, artifact := range binding.Artifacts {
			covered[artifact.Group.ID()] = true
		}
	}
	for id := range declared {
		if !covered[id] && !inert[id] {
			return coverage, nil, fmt.Errorf("%w: checksum declaration %s is not covered by the observed contexts", errProbeInconclusive, id)
		}
	}
	return coverage, nil, nil
}
