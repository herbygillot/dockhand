package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
)

func (s *Service) assessArchives(ctx context.Context, request Request, input *sourceInput) (coverage []ContextCoverage, fetchErr, checksumErr error) {
	if gitFetched(input.info) {
		return []ContextCoverage{{Fetch: input.info.Fetch, Platform: input.before.Platform}}, checkGitSource(input.info), nil
	}
	profiles, err := s.contextProfiles(ctx, request, input, input.data)
	if err != nil {
		return coverage, err, nil
	}
	declared, covered := map[string]bool{}, map[string]bool{}
	observations, err := s.observeProfiles(ctx, input, input.data, profiles, true, false)
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
		if err := checkArchivePolicy(info, input.portdir()); err != nil {
			return coverage, err, nil
		}
		metadata, inconclusive := tolerateExplainedProbes(ctx, observed.Ports[input.target.Name], input.data, input.files.root)
		if inconclusive {
			return coverage, fmt.Errorf("%w: modeled context depends on host state%s", errProbeInconclusive, hostInputs(metadata)), nil
		}
		if len(metadata.Problems) > 0 {
			return coverage, fmt.Errorf("%w: %v", ErrUnsupported, metadata.Problems), nil
		}
		if len(metadata.Distfiles) == 0 {
			return coverage, fmt.Errorf("%w: no source archives; select a release subport if this is a metaport", ErrUnsupported), nil
		}
		binding, err := distfiles.Bind(input.data, input.portfile(), info, metadata)
		if err != nil {
			return coverage, nil, err
		}
		for _, group := range binding.Groups {
			declared[group.ID()] = true
		}
		for _, artifact := range binding.Artifacts {
			covered[artifact.Group.ID()] = true
		}
	}
	for id := range declared {
		if !covered[id] {
			return coverage, nil, fmt.Errorf("%w: checksum declaration %s is not covered by the observed contexts", errProbeInconclusive, id)
		}
	}
	return coverage, nil, nil
}
