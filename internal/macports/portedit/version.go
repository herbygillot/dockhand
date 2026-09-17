package portedit

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
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
	spec, err := portsource.ForEditing(input.info)
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
	if request.SharedRelease {
		input.scope, err = releaseScope(input.before, versioned, input.target.Name, true)
		if err != nil {
			return archivePlan{}, err
		}
		input.scope.Input = input.versionInput
	}
	if _, ok := s.Ports.(macports.Observer); ok {
		observed, err := s.planObservedArchives(ctx, request, input, contents)
		result := Result{Scope: input.scope, Base: request.Source, Target: input.target, Release: release, Fidelity: []Fidelity{scopedVersionFidelity(request.SharedRelease, input.before, versioned, input.target.Name, input.files.root, *release, versioned.Ports[input.target.Name].Options["checksums"])}}
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
	fidelity := scopedVersionFidelity(request.SharedRelease, input.before, versioned, input.target.Name, input.files.root, *release, info.Options["checksums"])
	result := Result{Scope: input.scope, Base: request.Source, Target: input.target, Release: release, Fidelity: []Fidelity{fidelity}}
	if len(fidelity.UnexpectedChanges) > 0 {
		return archivePlan{result: result}, fmt.Errorf("%w: %v", ErrFidelity, fidelity.UnexpectedChanges)
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
	return s.applyArchivePlan(ctx, request, input, plan, s.archives(""))
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
	contents, checksums, downloads, err := archives.refresh(ctx, contents, info, sources)
	if err != nil {
		return result, err
	}
	result.Downloads = downloads
	evaluated, err := s.evaluateEdit(ctx, input, contents)
	if err != nil {
		return result, err
	}
	final := checksumFidelity(versioned, evaluated.after, input.target.Name, input.files.root, checksums)
	return result, result.commitEdit(input, request, evaluated.edit, final, plan.subject)
}

func versionFidelity(before, after macports.Snapshot, selected, root string, release record.Release, checksums string) Fidelity {
	result := Fidelity{Before: before, After: after, ExpectedChanges: []string{selected + ".version -> " + release.Version, selected + ".revision -> 0", selected + ".distfiles and checksums"}, UnexpectedChanges: []string{}}
	names := map[string]bool{}
	for name := range before.Ports {
		names[name] = true
	}
	for name := range after.Ports {
		names[name] = true
	}
	for name := range names {
		original, was := before.Ports[name]
		next, is := after.Ports[name]
		if !was || !is {
			result.UnexpectedChanges = append(result.UnexpectedChanges, name+": port set changed")
			continue
		}
		old := comparablePort(original, root)
		next = comparablePort(next, root)
		if name == selected {
			old.Version, old.Revision = release.Version, 0
			for _, key := range []string{"fetch.has_credentials", "version", "github.version", "gitlab.version", "go.version", "git.branch", "distname", "dist_subdir", "distfiles", "extract.only", "master_sites", "worksrcdir", "livecheck.version", "github.master_sites", "gitlab.master_sites"} {
				if value, ok := next.Options[key]; ok {
					old.Options[key] = value
				} else {
					delete(old.Options, key)
				}
			}

			if release.Tag != "" && next.Options["git.branch"] != release.Tag {
				result.UnexpectedChanges = append(result.UnexpectedChanges, name+".git.branch differs from selected tag")
			}
			actual, errs := syntax.ListValues(next.Options["checksums"])
			expected, expectedErrs := syntax.ListValues(checksums)
			if len(errs) > 0 || len(expectedErrs) > 0 || !slices.Equal(actual, expected) {
				result.UnexpectedChanges = append(result.UnexpectedChanges, name+".checksums differ from intended values")
			}
			old.Options["checksums"] = next.Options["checksums"]
		}
		if old.Revision != next.Revision {
			result.UnexpectedChanges = append(result.UnexpectedChanges, name+".revision changed unexpectedly")
		}
		result.UnexpectedChanges = append(result.UnexpectedChanges, comparePortMetadata(name, old, next)...)
	}
	slices.Sort(result.UnexpectedChanges)
	return result
}

func checksumFidelity(before, after macports.Snapshot, selected, root, checksums string) Fidelity {
	expected := before
	expected.Ports = maps.Clone(before.Ports)
	info := expected.Ports[selected]
	info.Options = maps.Clone(info.Options)
	info.Options["checksums"] = checksums
	expected.Ports[selected] = info
	result := Fidelity{Before: before, After: after, ExpectedChanges: []string{selected + ".checksums"}}
	normalized := after
	normalized.Ports = maps.Clone(after.Ports)
	next := normalized.Ports[selected]
	next.Options = maps.Clone(next.Options)
	values, errs := syntax.ListValues(next.Options["checksums"])
	wanted, wantedErrs := syntax.ListValues(checksums)
	if len(errs) > 0 || len(wantedErrs) > 0 || !slices.Equal(values, wanted) {
		result.UnexpectedChanges = append(result.UnexpectedChanges, selected+".checksums differ from intended values")
	}
	next.Options["checksums"] = checksums
	normalized.Ports[selected] = next
	if err := CheckEquivalent(expected, normalized, root, root); err != nil {
		result.UnexpectedChanges = append(result.UnexpectedChanges, err.Error())
	}
	return result
}

func checksumValues(downloads []Download) []portfile.Checksum {
	values := make([]portfile.Checksum, len(downloads))
	for i, d := range downloads {
		values[i] = portfile.Checksum{Name: d.Name, SHA256: d.SHA256, RMD160: d.RMD160, Size: d.Size}
	}
	return values
}
