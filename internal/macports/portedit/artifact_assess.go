package portedit

import (
	"context"

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
	left := false
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
		// A context modelled on another Mac that can't be read is left, as
		// update leaves it, and said (#125); this Mac's reading isn't.
		if reason, leaves := observe.Left(ctx, observed, input.target.Name, input.data); leaves {
			contexts[len(contexts)-1].Left = reason
			left = true
			continue
		}
		if obsoleteIn(info) {
			// An obsolete follower has no archive in this context by design.
			continue
		}
		if err := distfetch.CheckPolicy(info, input.portdirIn(observed.Snapshot.Root)); err != nil {
			return contexts, err, nil
		}
		metadata, inconclusive := observe.Tolerate(ctx, observed.Ports[input.target.Name], input.data, observed.Snapshot.Root)
		if inconclusive {
			return contexts, errProbeInconclusive.Because("modeled context depends on host state%s", observe.HostInputs(metadata)), nil
		}
		if len(metadata.Problems) > 0 {
			return contexts, ErrUnsupported.Because("%v", metadata.Problems), nil
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
	// A declaration only a context left fetches stays as written, as
	// update leaves it.
	ids := coverage.uncovered()
	if len(ids) > 0 && !left {
		return contexts, nil, errProbeInconclusive.Because("checksum declaration %s is not covered by the observed contexts", ids[0])
	}
	for i := range contexts {
		if contexts[i].Left != "" {
			contexts[i].Kept = ids
		}
	}
	return contexts, nil, nil
}
