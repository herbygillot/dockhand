package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"maps"
	"os"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
)

type archivePlan struct {
	observed  *observedArchivePlan
	result    Result
	contents  []byte
	versioned macports.Snapshot
	sources   []archiveSource
	// subject names the commit the applied plan intends.
	subject string
}

func (s *Service) planArchiveVersion(ctx context.Context, request Request, input *sourceInput) (archivePlan, error) {
	release := request.Release
	if release == nil || release.Requested != request.Version {
		return archivePlan{}, fmt.Errorf("portedit: a matching resolved release is required")
	}
	if request.Version == "" && release.CurrentVersion != input.info.Version {
		return archivePlan{}, fmt.Errorf("portedit: automatic selection does not match the input version")
	}
	if release.NoUpdate && request.Version != "" {
		return archivePlan{}, fmt.Errorf("portedit: explicit selection cannot imply no update")
	}
	if release.NoUpdate {
		return archivePlan{result: Result{Base: request.Source, Target: input.target, Release: release}}, nil
	}
	spec, err := portsource.Interpret(input.info, portsource.Edit)
	if err != nil {
		return archivePlan{}, err
	}
	sourceVersion, ok := spec.Pattern.Version(release.Tag)
	if spec.Forge == "" {
		sourceVersion, ok = release.Version, release.Forge == ""
	}
	if !ok {
		return archivePlan{}, fmt.Errorf("%w: selected tag does not match source convention", ErrFidelity)
	}
	carriers, err := s.versionCarriers(ctx, request, input)
	if err != nil {
		return archivePlan{}, err
	}
	contents, versioned, err := s.probeVersion(ctx, request, input, carriers, sourceVersion)
	if err != nil {
		return archivePlan{}, err
	}
	if versioned.Ports[input.target.Name].Version != release.Version {
		return archivePlan{}, fmt.Errorf("%w: evaluated version differs from resolved release", ErrFidelity)
	}
	scope, err := fidelity.ReleaseScope(input.before, versioned, input.target.Name, request.SharedRelease)
	if err != nil {
		return archivePlan{}, err
	}
	// The scope is recorded when it holds more than the target: an authorized
	// shared release, or obsolete followers that moved with the target.
	if request.SharedRelease || len(scope.Affected) > 1 {
		scope.Input = input.versionInput
		input.scope = scope
	}
	if _, ok := s.Ports.(macports.Observer); ok {
		observed, err := s.planObservedArchives(ctx, request, input, contents)
		result := Result{Scope: input.scope, Base: request.Source, Target: input.target, Release: release, Fidelity: []Fidelity{fidelity.ScopedVersion(request.SharedRelease, input.before, versioned, input.target.Name, input.files.root, *release, versioned.Ports[input.target.Name].Options["checksums"])}}
		if observed != nil {
			for _, frame := range observed.contexts {
				result.Coverage = append(result.Coverage, ContextCoverage{Fetch: frame.after.Ports[input.target.Name].Fetch, Platform: frame.profile, Modeled: frame.profile != input.before.Platform, Affected: frame.affected})
			}
		}
		return archivePlan{result: result, contents: contents, versioned: versioned, observed: observed, subject: "update to " + release.Version}, err
	}

	oldSources, err := downloadSources(input.info, input.portdir())
	if err != nil {
		return archivePlan{}, err
	}
	oldGroups, err := portfile.ChecksumCount(input.data, input.info.Options["checksums"])
	if err != nil {
		return archivePlan{}, err
	}

	info := versioned.Ports[input.target.Name]
	checked := info
	checked.Options = maps.Clone(info.Options)
	checked.Options["filespath"] = input.info.Options["filespath"]
	sources, err := downloadSources(checked, input.portdir())
	if err != nil {
		return archivePlan{}, err
	}
	groups, err := portfile.ChecksumCount(contents, info.Options["checksums"])
	if err != nil {
		return archivePlan{}, err
	}
	if groups != oldGroups || len(sources) != len(oldSources) {
		return archivePlan{}, fmt.Errorf("%w: source/checksum group count changed", ErrUnsupported)
	}
	changed := false
	for i, source := range sources {
		if source.URL != oldSources[i].URL {
			changed = true
		}
	}
	if !changed {
		return archivePlan{}, fmt.Errorf("%w: version edit did not change the download source", ErrUnsupported)
	}
	report := fidelity.ScopedVersion(request.SharedRelease, input.before, versioned, input.target.Name, input.files.root, *release, info.Options["checksums"])
	result := Result{Scope: input.scope, Base: request.Source, Target: input.target, Release: release, Fidelity: []Fidelity{report}}
	if len(report.UnexpectedChanges) > 0 {
		return archivePlan{result: result}, fmt.Errorf("%w: %v", ErrFidelity, report.UnexpectedChanges)
	}
	if err := checkChecksumSources(contents, info, sources); err != nil {
		return archivePlan{}, err
	}
	return archivePlan{result: result, contents: contents, versioned: versioned, sources: sources, subject: "update to " + release.Version}, nil
}

func (s *Service) prepareArchiveVersion(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	plan, err := s.planArchiveVersion(ctx, request, input)
	if err != nil {
		return plan.result, err
	}
	store := s.archives("")
	if patched(input.info) {
		directory, err := os.MkdirTemp("", "dockhand-patchcheck-")
		if err != nil {
			return Result{}, err
		}
		defer os.RemoveAll(directory)
		store = s.archives(directory)
	}
	result, err := s.applyArchivePlan(ctx, request, input, plan, store)
	if err != nil {
		return result, err
	}
	return result, s.checkPatches(ctx, input, &result)
}

func (s *Service) applyArchivePlan(ctx context.Context, request Request, input *sourceInput, plan archivePlan, archives *archiveStore) (Result, error) {
	result := plan.result
	if request.Release.NoUpdate {
		return result, nil
	}
	if plan.observed != nil {
		return s.applyObservedArchives(ctx, request, input, plan, archives)
	}
	contents, versioned, sources := plan.contents, plan.versioned, plan.sources
	info := versioned.Ports[input.target.Name]
	contents, checksums, downloads, err := archives.refresh(ctx, contents, info, sources, request.KeepOldChecksums)
	if err != nil {
		return result, err
	}
	result.Downloads = downloads
	evaluated, err := s.evaluateEdit(ctx, input, contents)
	if err != nil {
		return result, err
	}
	final := fidelity.Checksums(versioned, evaluated.after, input.target.Name, input.files.root, checksums)
	return result, result.commitEdit(input, request, evaluated.edit, final, plan.subject)
}

func checksumValues(downloads []Download) []portfile.Checksum {
	values := make([]portfile.Checksum, len(downloads))
	for i, d := range downloads {
		values[i] = portfile.Checksum{Name: d.Name, SHA256: d.SHA256, RMD160: d.RMD160, MD5: d.MD5, SHA1: d.SHA1, Size: d.Size}
	}
	return values
}
