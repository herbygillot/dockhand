package portedit

import (
	"context"

	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
)

// assessArchives asks for a judged plan: on main it left the context, applied
// archive policy, and required a conclusive verdict before measuring coverage.
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
		need, policyWithoutArchives := observe.FetchJudgedPlan, true
		if obsoleteIn(observed.Snapshot.Ports[input.target.Name]) {
			// Main left an obsolete follower before applying archive policy or
			// judging its plan; keep only the leave check for that context.
			need, policyWithoutArchives = observe.FetchRevisionOnly, false
		}
		decision, err := observe.DecideFetch(ctx, observed, input.target.Name, input.data, input.portdirIn(observed.Snapshot.Root), need, policyWithoutArchives, nil)
		if err != nil {
			return contexts, err, nil
		}
		info := decision.Info
		contexts = append(contexts, ContextCoverage{Platform: profile, Modeled: observed.Modeled, Fetch: info.Fetch})
		if decision.Kind == observe.FetchLeft {
			contexts[len(contexts)-1].Left = decision.Reason
			left = true
			continue
		}
		if obsoleteIn(info) {
			// An obsolete follower has no archive in this context by design.
			continue
		}
		if decision.Kind == observe.FetchRefused {
			return contexts, decision.Err, nil
		}
		if decision.Kind == observe.FetchInconclusive {
			return contexts, errProbeInconclusive.Because("%s", decision.Reason), nil
		}
		metadata := decision.Metadata
		if decision.Kind == observe.FetchNoPlan && decision.HadProblems {
			return contexts, ErrUnsupported.Because("%v", metadata.Problems), nil
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
