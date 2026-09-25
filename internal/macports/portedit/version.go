package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/scratch"
	"os"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	portsource "github.com/herbygillot/dockhand/internal/macports/source"
)

type archivePlan struct {
	observed  *observedArchivePlan
	result    Result
	contents  []byte
	versioned macports.Snapshot
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
	// The scope is recorded when it holds more than the target: siblings
	// sharing the release, or obsolete followers that moved with the target.
	// A release of one port records none, authorized or not.
	if len(scope.Affected) > 1 {
		scope.Input = input.versionInput
		input.scope = scope
	}
	if gitFetched(input.info) {
		return s.planGitVersion(ctx, request, input, contents, versioned)
	}
	observed, err := s.planObservedArchives(ctx, request, input, contents)
	result := Result{Scope: input.scope, Base: request.Source, Target: input.target, Release: release}
	result.report(fidelity.ScopedVersion(request.SharedRelease, family, versioned, input.target.Name, *release, versioned.Ports[input.target.Name].Options["checksums"]))
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
	store := s.Archives.Store("")
	switch {
	case request.KeepArchives != "":
		store = s.Archives.Store(request.KeepArchives)
	case patched(input.info) || moduleModeGo(input.info):
		directory, err := scratch.Dir("patchcheck-")
		if err != nil {
			return Result{}, err
		}
		defer os.RemoveAll(directory)
		store = s.Archives.Store(directory)
	}
	result, err := s.applyArchivePlan(ctx, request, input, plan, store)
	if err != nil {
		return result, err
	}
	if request.KeepArchives != "" {
		result.Previous, result.PreviousProblem = s.previousArchives(ctx, input, store)
	}
	if err := s.raiseGoToolchain(ctx, request, input, &result); err != nil {
		return result, err
	}
	return result, s.checkPatches(ctx, input, &result)
}

func (s *Service) applyArchivePlan(ctx context.Context, request Request, input *sourceInput, plan archivePlan, store *archives.Store) (Result, error) {
	result := plan.result
	if request.Release.NoUpdate {
		return result, nil
	}
	if plan.viaGit {
		return s.applyGitVersion(ctx, request, input, plan)
	}
	if plan.observed == nil {
		// Every archive plan that is not a Git plan carries its observed
		// contexts, since the observation runs on every plan; a plan
		// without them is a programming error, not a path.
		return result, fmt.Errorf("portedit: archive plan without observed contexts")
	}
	return s.applyObservedArchives(ctx, request, input, plan, store)
}

// previousArchives fetches the current version's archives, for comparing
// with the new ones. Not getting them is a problem to report, never a
// reason to refuse the update.
func (s *Service) previousArchives(ctx context.Context, input *sourceInput, store *archives.Store) ([]archives.Download, string) {
	sources, err := archives.Sources(input.info, input.portdirIn(input.before.Root))
	if err != nil {
		return nil, err.Error()
	}
	var kept []archives.Download
	for _, source := range sources {
		download, err := store.Fetch(ctx, input.info, source)
		if err != nil {
			return kept, fmt.Sprintf("fetching %s: %v", source.Name, err)
		}
		kept = append(kept, download)
	}
	return kept, ""
}
