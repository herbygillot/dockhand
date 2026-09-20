package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
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
	// viaGit marks a git-fetched port's plan, which downloads nothing;
	// branch is where git.branch must land, empty when the tag is unknown.
	viaGit bool
	branch string
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
		sourceVersion, ok = release.SourceSpelling(), release.Forge == ""
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
	family, err := input.familySnapshot(ctx, s.Ports)
	if err != nil {
		return archivePlan{}, err
	}
	scope, err := fidelity.ReleaseScope(family, versioned, input.target.Name, request.SharedRelease)
	if err != nil {
		return archivePlan{}, err
	}
	// The scope is recorded when it holds more than the target: an authorized
	// shared release, or obsolete followers that moved with the target.
	if request.SharedRelease || len(scope.Affected) > 1 {
		scope.Input = input.versionInput
		input.scope = scope
	}
	if gitFetched(input.info) {
		return s.planGitVersion(ctx, request, input, contents, versioned)
	}
	observed, err := s.planObservedArchives(ctx, request, input, contents)
	result := Result{Scope: input.scope, Base: request.Source, Target: input.target, Release: release}
	result.report(fidelity.ScopedVersion(request.SharedRelease, family, versioned, input.target.Name, input.files.root, *release, versioned.Ports[input.target.Name].Options["checksums"]))
	if observed != nil {
		for _, frame := range observed.contexts {
			result.Coverage = append(result.Coverage, ContextCoverage{Fetch: frame.after.Ports[input.target.Name].Fetch, Platform: frame.profile, Modeled: frame.profile != input.before.Platform, Affected: frame.affected})
		}
	}
	return archivePlan{result: result, contents: contents, versioned: versioned, observed: observed, subject: "update to " + release.Version}, err
}

func (s *Service) prepareArchiveVersion(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	plan, err := s.planArchiveVersion(ctx, request, input)
	if err != nil {
		return plan.result, err
	}
	store := s.archives("")
	if patched(input.info) || moduleModeGo(input.info) {
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
	if err := s.raiseGoToolchain(ctx, request, input, &result); err != nil {
		return result, err
	}
	return result, s.checkPatches(ctx, input, &result)
}

func (s *Service) applyArchivePlan(ctx context.Context, request Request, input *sourceInput, plan archivePlan, archives *archiveStore) (Result, error) {
	result := plan.result
	if request.Release.NoUpdate {
		return result, nil
	}
	if plan.viaGit {
		return s.applyGitVersion(ctx, request, input, plan)
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
