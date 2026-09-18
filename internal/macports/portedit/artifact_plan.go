package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

type archiveContext struct {
	profile       record.Platform
	before, after macports.Snapshot
	binding       distfiles.Binding
	affected      bool
}
type plannedArchive struct {
	artifact distfiles.Artifact
	info     macports.PortInfo
}
type observedArchivePlan struct {
	contexts  []archiveContext
	downloads []plannedArchive
}

func (s *Service) bindArchives(input *sourceInput, contents []byte, observed macports.Observation) (distfiles.Binding, error) {
	port, inconclusive := tolerateToolchainProbes(observed.Ports[input.target.Name], contents)
	if inconclusive {
		return distfiles.Binding{}, fmt.Errorf("%w: modeled context depends on host state%s", errProbeInconclusive, hostInputs(port))
	}
	info := observed.Snapshot.Ports[input.target.Name]
	if err := checkArchivePolicy(info, input.portdir()); err != nil {
		return distfiles.Binding{}, err
	}
	binding, err := distfiles.Bind(contents, input.portfile(), info, port)
	if err == nil && len(binding.Artifacts) == 0 {
		err = fmt.Errorf("%w: no downloadable source archives; select a release subport when this is a metaport", ErrUnsupported)
	}
	return binding, err
}

func (s *Service) planObservedArchives(ctx context.Context, request Request, input *sourceInput, contents []byte) (*observedArchivePlan, error) {
	profiles, err := s.contextProfiles(ctx, request, input, contents)
	if err != nil {
		return nil, err
	}
	plan := &observedArchivePlan{}
	covered := map[string]bool{}
	declared := map[string]bool{}
	protected := map[string]bool{}
	changed := map[string]bool{}
	unique := map[string]bool{}
	progress.DebugReport(ctx, "Observing %d archive contexts", len(profiles))
	befores, err := s.observeProfiles(ctx, input, input.data, profiles, true, false)
	if err != nil {
		return nil, fmt.Errorf("%w: observing baseline %v", errProbeInconclusive, err)
	}
	afters, err := s.observeProfiles(ctx, input, contents, profiles, true, false)
	if err != nil {
		return nil, fmt.Errorf("%w: observing candidate %v", errProbeInconclusive, err)
	}
	for i, profile := range profiles {
		progress.DebugReport(ctx, "Checking archive context %s %s %s", profile.OS, profile.Version, profile.Architecture)
		before, after := befores[i], afters[i]
		old, next := before.Snapshot.Ports[input.target.Name], after.Snapshot.Ports[input.target.Name]
		if obsoleteIn(old) && obsoleteIn(next) {
			// The port is an obsolete follower in this context, with no
			// archive of its own; the context adds no requirement.
			progress.VerboseReport(ctx, "%s is obsolete on %s %s %s; no archive to cover there", input.target.Name, profile.OS, profile.Version, profile.Architecture)
			continue
		}
		affected := old.Version != next.Version
		if next.Fetch != nil && next.Fetch.Rejected {
			progress.VerboseReport(ctx, "Preserving rejection-only fetch guard for %s %s %s; archive coverage does not establish build support", profile.OS, profile.Version, profile.Architecture)
		}
		if affected && (old.Version != input.info.Version || next.Version != request.Release.Version) {
			return nil, fmt.Errorf("%w: candidate changed an independent version on %+v", ErrFidelity, profile)
		}
		if !affected {
			if err := fidelity.Equivalent(before.Snapshot, after.Snapshot, input.files.root, input.files.root); err != nil {
				return nil, fmt.Errorf("protected context %+v: %w", profile, err)
			}
		} else {
			report := fidelity.ScopedVersion(request.SharedRelease, before.Snapshot, after.Snapshot, input.target.Name, input.files.root, *request.Release, next.Options["checksums"])
			if len(report.UnexpectedChanges) > 0 {
				return nil, fmt.Errorf("%w: context %+v: %v", ErrFidelity, profile, report.UnexpectedChanges)
			}
		}
		oldBinding, err := s.bindArchives(input, input.data, before)
		if err != nil {
			return nil, fmt.Errorf("baseline %+v: %w", profile, err)
		}
		binding, err := s.bindArchives(input, contents, after)
		if err != nil {
			return nil, fmt.Errorf("candidate %+v: %w", profile, err)
		}
		if err := s.checkSharedArchiveOwners(input, input.data, before, oldBinding); err != nil {
			return nil, err
		}
		if err := s.checkSharedArchiveOwners(input, contents, after, binding); err != nil {
			return nil, err
		}
		if len(oldBinding.Groups) != len(binding.Groups) || len(oldBinding.Artifacts) != len(binding.Artifacts) {
			return nil, fmt.Errorf("%w: candidate changed archive/checksum structure", ErrFidelity)
		}
		oldGroups := map[string]distfiles.Group{}
		for _, group := range oldBinding.Groups {
			oldGroups[group.ID()] = group
		}
		for _, group := range binding.Groups {
			previous, ok := oldGroups[group.ID()]
			if !ok || len(previous.Values) != len(group.Values) {
				return nil, fmt.Errorf("%w: candidate activated different checksum declarations", ErrFidelity)
			}
			for algorithm, value := range group.Values {
				if previous.Values[algorithm].Value != value.Value {
					return nil, fmt.Errorf("%w: candidate changed checksum literals", ErrFidelity)
				}
			}
			declared[group.ID()] = true
			if !affected {
				protected[group.ID()] = true
			}
		}
		oldFiles := map[string]distfiles.Artifact{}
		for _, artifact := range oldBinding.Artifacts {
			oldFiles[artifact.Group.ID()] = artifact
		}
		for _, artifact := range binding.Artifacts {
			id := artifact.Group.ID()
			covered[id] = true
			previous, ok := oldFiles[id]
			if !ok {
				return nil, fmt.Errorf("%w: candidate changed archive ownership", ErrFidelity)
			}
			if slices.Equal(previous.URLs, artifact.URLs) {
				protected[id] = true
				if previous.Name != artifact.Name {
					return nil, fmt.Errorf("%w: filename changed without fetch location", ErrFidelity)
				}
				continue
			}
			if !affected {
				return nil, fmt.Errorf("%w: protected artifact location changed", ErrFidelity)
			}
			changed[id] = true
			key := id + "\x00" + artifact.Name + "\x00" + strings.Join(artifact.URLs, "\x00")
			if !unique[key] {
				plan.downloads = append(plan.downloads, plannedArchive{artifact: artifact, info: next})
				unique[key] = true
			}
		}
		plan.contexts = append(plan.contexts, archiveContext{profile: profile, before: before.Snapshot, after: after.Snapshot, binding: binding, affected: affected})
	}
	for id := range declared {
		if !covered[id] {
			return nil, fmt.Errorf("%w: checksum declaration %s has no archive in the observed contexts", errProbeInconclusive, id)
		}
	}
	for id := range changed {
		if protected[id] {
			return nil, fmt.Errorf("%w: checksum declaration %s is shared with a protected archive", ErrFidelity, id)
		}
	}
	if len(plan.downloads) == 0 {
		return nil, fmt.Errorf("%w: version edit did not change the download source", ErrUnsupported)
	}
	return plan, nil
}
