package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
)

func (s *Service) assessArchives(ctx context.Context, request Request, input *sourceInput) (contexts []ContextCoverage, fetchErr, checksumErr error) {
	if input.info.GitFetched() {
		return []ContextCoverage{{Fetch: input.info.Fetch, Platform: input.before.Platform}}, checkGitSource(input.info), nil
	}
	profiles, err := input.observe.Profiles(ctx, input.data)
	if err != nil {
		return contexts, err, nil
	}
	coverage := newArchiveCoverage()
	// Only the selected port is read below, so each context evaluates it
	// alone; the family is for edits, whose fidelity must see siblings.
	observations, err := input.observe.Observe(ctx, input.data, profiles, true, true)
	if err != nil {
		return contexts, err, nil
	}
	for i, profile := range profiles {
		observed := observations[i]
		info := observed.Snapshot.Ports[input.target.Name]
		contexts = append(contexts, ContextCoverage{Platform: profile, Modeled: observed.Modeled, Fetch: info.Fetch})
		if obsoleteIn(info) {
			// An obsolete follower has no archive in this context by design.
			continue
		}
		if err := distfetch.CheckPolicy(info, input.portdirIn(observed.Snapshot.Root)); err != nil {
			return contexts, err, nil
		}
		metadata, inconclusive := observe.Tolerate(ctx, observed.Ports[input.target.Name], input.data, observed.Snapshot.Root)
		if inconclusive {
			return contexts, fmt.Errorf("%w: modeled context depends on host state%s", errProbeInconclusive, observe.HostInputs(metadata)), nil
		}
		if len(metadata.Problems) > 0 {
			return contexts, fmt.Errorf("%w: %v", ErrUnsupported, metadata.Problems), nil
		}
		if len(metadata.Distfiles) == 0 {
			// Nothing to download here, as a metaport or a _select
			// port has; the context adds no archive to cover.
			contexts[len(contexts)-1].FetchesNothing = true
			continue
		}
		binding, err := distfiles.Bind(input.data, input.portfileIn(observed.Snapshot.Root), info, metadata)
		if err != nil {
			return contexts, nil, err
		}
		coverage.declare(info, binding.Groups)
		for _, artifact := range binding.Artifacts {
			coverage.cover(artifact)
		}
	}
	if ids := coverage.uncovered(); len(ids) > 0 {
		return contexts, nil, fmt.Errorf("%w: checksum declaration %s is not covered by the observed contexts", errProbeInconclusive, ids[0])
	}
	return contexts, nil, nil
}
