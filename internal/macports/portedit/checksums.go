package portedit

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/progress"
)

// prepareChecksums re-downloads every declared archive and rewrites the
// checksums it owns. With an observer, every modeled context is covered, so
// per-platform distfiles are refreshed the same way a version bump refreshes
// them; without one, the plain declaration path handles a single context.
func (s *Service) prepareChecksums(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	result := Result{Base: request.Source, Target: input.target}
	observed, err := s.planObservedChecksums(ctx, request, input)
	if err != nil {
		return result, err
	}
	for _, frame := range observed.contexts {
		result.Coverage = append(result.Coverage, ContextCoverage{Fetch: frame.after.Ports[input.target.Name].Fetch, Platform: frame.profile, Modeled: frame.profile != input.before.Platform})
	}
	return s.applyObservedArchives(ctx, request, input, archivePlan{result: result, contents: input.data, observed: observed, subject: "refresh checksums"}, s.archives(""))
}

func (s *Service) planObservedChecksums(ctx context.Context, request Request, input *sourceInput) (*observedArchivePlan, error) {
	profiles, err := s.contextProfiles(ctx, request, input, input.data)
	if err != nil {
		return nil, err
	}
	plan := &observedArchivePlan{}
	declared, covered, unique := map[string]bool{}, map[string]bool{}, map[string]bool{}
	observations, err := s.observeProfiles(ctx, input, input.data, profiles, true, false)
	if err != nil {
		return nil, fmt.Errorf("%w: observing %v", errProbeInconclusive, err)
	}
	for i, profile := range profiles {
		progress.DebugReport(ctx, "Checking archive context %s %s %s", profile.OS, profile.Version, profile.Architecture)
		observed := observations[i]
		if obsoleteIn(observed.Snapshot.Ports[input.target.Name]) {
			continue
		}
		binding, err := s.bindArchives(ctx, input, input.data, observed)
		if err != nil {
			return nil, fmt.Errorf("context %+v: %w", profile, err)
		}
		if err := s.checkSharedArchiveOwners(ctx, input, input.data, observed, binding); err != nil {
			return nil, err
		}
		info := observed.Snapshot.Ports[input.target.Name]
		for _, group := range binding.Groups {
			declared[group.ID()] = true
		}
		for _, artifact := range binding.Artifacts {
			covered[artifact.Group.ID()] = true
			key := artifact.Group.ID() + "\x00" + artifact.Name + "\x00" + strings.Join(artifact.URLs, "\x00")
			if !unique[key] {
				plan.downloads = append(plan.downloads, plannedArchive{artifact: artifact, info: info})
				unique[key] = true
			}
		}
		plan.contexts = append(plan.contexts, archiveContext{profile: profile, before: observed.Snapshot, after: observed.Snapshot, binding: binding})
	}
	for id := range declared {
		if !covered[id] {
			return nil, fmt.Errorf("%w: checksum declaration %s has no archive in the observed contexts", errProbeInconclusive, id)
		}
	}
	return plan, nil
}

func checkChecksumSources(contents []byte, info macports.PortInfo, sources []archiveSource) error {
	placeholders := make([]portfile.Checksum, len(sources))
	for i, source := range sources {
		placeholders[i] = portfile.Checksum{Name: source.Name}
	}
	_, _, err := portfile.ReplaceChecksums(contents, info.Options["checksums"], placeholders...)
	return err
}
