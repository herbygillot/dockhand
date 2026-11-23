package portedit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/macports/portsource"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/scratch"
)

type archivePlan struct {
	observed *observedArchivePlan
	result   Result
	contents []byte
	// viaGit marks a git-fetched port's plan, which downloads nothing;
	// branch is where git.branch must land, empty when the tag is unknown.
	viaGit bool
	branch string
	// subject names the commit the applied plan intends.
	subject string
	// fetched are archives choosing the source downloaded already, which
	// the plan's downloads take rather than fetching again.
	fetched []distfetch.Download
}

func (s *Service) planArchiveVersion(ctx context.Context, request Request, input *sourceInput) (archivePlan, error) {
	release := request.Release
	if release == nil || release.Requested != request.Version {
		return archivePlan{}, fmt.Errorf("portedit: a matching resolved release is required")
	}
	if request.Version == "" && release.CurrentVersion != input.info.Version {
		return archivePlan{}, fmt.Errorf("portedit: automatic selection does not match the input version")
	}
	// An explicit selection implies no update only where it names the
	// version the port is at (#165).
	if release.NoUpdate && request.Version != "" && release.Version != input.info.Version {
		return archivePlan{}, fmt.Errorf("portedit: explicit selection cannot imply no update")
	}
	if release.NoUpdate && request.ActOnCurrent && (request.SourceMode == SourceReleases || request.SourceMode == SourceArchive) {
		return s.planSourceOnCurrent(ctx, request, input)
	}
	if release.NoUpdate {
		// The family as it stands, so what a current version leaves to do
		// beside it is found: an obsolete stub that --with-obsolete moves,
		// which nothing found where the family was empty (#156).
		result := Result{Target: input.target, Release: release}
		if family, err := input.familySnapshot(ctx, s.Ports); err == nil {
			result.Fidelity = []Fidelity{{Before: family, After: family}}
		}
		return archivePlan{result: result}, nil
	}
	// A release at the version the port already evaluates to, which
	// nothing marked current, as where the version was set by hand on the
	// branch the update reads (watchman's), has no version input to move:
	// said so, before any is searched, where it said no version input could
	// select it (#229). A commit pin moves its commit at the same version.
	if !release.Pinned && release.Version == input.info.Version {
		return archivePlan{}, ErrUnsupported.Because("%s is already at %s, so update has no version to move; where the version was set by hand, dockhand checksums %s refreshes its checksums", input.target.Name, release.Version, input.target.Name)
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
		return archivePlan{}, ErrFidelity.Because("selected tag does not match source convention")
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
		return archivePlan{}, ErrFidelity.Because("evaluated version differs from resolved release")
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
	choice, err := s.chooseSource(ctx, request, input, contents, versioned)
	if err != nil {
		return archivePlan{contents: contents}, err
	}
	contents, versioned, input.sourceMoved = choice.contents, choice.after, choice.moved
	if input.sourceMoved == nil && input.tarballMoved {
		input.sourceMoved = &MovedSource{From: "tarball", To: "archive"}
	}
	if input.info.GitFetched() && input.sourceMoved == nil {
		plan, err := s.planGitVersion(ctx, request, input, contents, versioned)
		plan.result.SourceNotice = choice.notice
		return plan, err
	}
	observed, err := s.planObservedArchives(ctx, request, input, contents)
	result := Result{Scope: input.scope, Target: input.target, Release: release, SourceMoved: input.sourceMoved, SourceNotice: choice.notice}
	result.report(scopedVersion(request, input, family, versioned, input.target.Name, versioned.Ports[input.target.Name].Options["checksums"]))
	if observed != nil {
		// The contexts it couldn't read, and the declarations only they
		// fetch, left as the Portfile has them (#125).
		result.Left = observed.left
	}
	return archivePlan{result: result, contents: contents, observed: observed, fetched: choice.fetched, subject: "update to " + release.Version}, err
}

// scopedVersion is the version's fidelity, a source move's where the
// update moved the target's source (fidelity.ScopedSourceMove), and the
// move alone where the version stayed (fidelity.SourceMove).
func scopedVersion(request Request, input *sourceInput, before, after macports.Snapshot, selected, checksums string) fidelity.Report {
	// A source moved at the version the port is at keeps its version
	// (planSourceOnCurrent).
	if request.Release.NoUpdate && input.sourceMoved != nil {
		return fidelity.SourceMove(before, after, selected, checksums)
	}
	if input.sourceMoved != nil && selected == input.target.Name {
		return fidelity.ScopedSourceMove(request.SharedRelease, before, after, selected, *request.Release, checksums)
	}
	return fidelity.ScopedVersion(request.SharedRelease, before, after, selected, *request.Release, checksums)
}

func (s *Service) prepareArchiveVersion(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	plan, err := s.planArchiveVersion(ctx, request, input)
	if err != nil {
		// The version as the plan wrote it, for a hand edit's attempt.
		plan.result.Versioned = plan.contents
		return plan.result, err
	}
	store := s.Archives.Store("")
	switch {
	case request.KeepArchives != "":
		store = s.Archives.Store(request.KeepArchives)
	case patched(input.info) || input.info.GoModuleMode():
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
		var err error
		result.Pairs, err = pairArchives(ctx, store, plan.pairs(), nil, result.Downloads)
		result.previousFailed(err)
	}
	if err := s.raiseGoToolchain(ctx, request, input, &result); err != nil {
		return result, err
	}
	if err := s.checkPatches(ctx, input, &result, request.Attended); err != nil {
		return result, err
	}
	if err := s.dropMergedPatches(ctx, input, &result); err != nil {
		return result, err
	}
	return result, s.refreshPatches(ctx, input, &result)
}

func (s *Service) applyArchivePlan(ctx context.Context, request Request, input *sourceInput, plan archivePlan, store *distfetch.Store) (Result, error) {
	result := plan.result
	if request.Release.NoUpdate && plan.observed == nil {
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

// pairs are the archives the plan replaces, each beside its replacement;
// none for a git-fetched port, which downloads nothing.
func (p archivePlan) pairs() []archivePair {
	if p.observed == nil {
		return nil
	}
	return p.observed.pairs
}

// ArchivePair is an archive an update replaced, as MacPorts shipped it,
// beside the archive that replaces it: what the upstream comparison reads.
// Base and Port are the port as the context that fetches them evaluates
// it, before the edit and after, whose build systems and worksrcdir are
// that context's.
type ArchivePair struct {
	Previous, Next distfetch.Download
	Base, Port     macports.PortInfo
}

// pairArchives fetches the archives the update replaces as MacPorts
// shipped them, checked against the Portfile's checksums, from upstream or
// else MacPorts' mirror (Store.Shipped), each as the context that fetches
// it declares it, and pairs each with the archive that replaces it there.
// have are archives already fetched as shipped, found by name. Not getting
// one is a problem to report, never a reason to refuse the update.
func pairArchives(ctx context.Context, store *distfetch.Store, pairs []archivePair, have, downloads []distfetch.Download) ([]ArchivePair, error) {
	named := func(downloads []distfetch.Download, name string) (distfetch.Download, bool) {
		i := slices.IndexFunc(downloads, func(d distfetch.Download) bool { return d.Name == name })
		if i < 0 {
			return distfetch.Download{}, false
		}
		return downloads[i], true
	}
	var paired []ArchivePair
	for _, pair := range pairs {
		next, ok := named(downloads, pair.next)
		if !ok {
			return nil, errors.New(pair.next + " wasn't fetched")
		}
		previous, ok := named(have, pair.previous.Name)
		if !ok {
			shipped, err := store.Shipped(ctx, pair.info, []macports.Distfile{pair.previous.Distfile})
			if err != nil {
				return nil, err
			}
			previous = shipped[0].Download
		}
		paired = append(paired, ArchivePair{Previous: previous, Next: next, Base: pair.info, Port: pair.port})
	}
	return paired, nil
}

// previousFailed records why the archives the update replaces couldn't be
// fetched, and whether another try may fetch them; nothing where err is
// nil.
func (r *Result) previousFailed(err error) {
	if err != nil {
		r.PreviousProblem, r.PreviousKind = err.Error(), refusal.KindOf(err)
	}
}

// shippedPlan is MacPorts' own fetch plan for the Portfile as it stands,
// on this Mac: each archive, with the locations MacPorts would fetch it
// from, a mirror group such as PyPI's expanded as MacPorts expands it. It
// asks for a plan, as main did, without judging native host access. The new
// version's archives are fetched from its plan the same way.
func shippedPlan(ctx context.Context, input *sourceInput) ([]macports.Distfile, error) {
	observed := macports.Observation{
		Snapshot: macports.Snapshot{Root: input.before.Root, Ports: map[string]macports.PortInfo{input.target.Name: input.info}},
		Ports:    map[string]macports.PortObservation{input.target.Name: {}},
	}
	decision, err := observe.DecideFetch(ctx, observed, input.target.Name, input.data, input.portdirIn(input.before.Root), observe.FetchPlanNeed, false, func(ctx context.Context) (macports.Observation, error) {
		if input.observe == nil {
			return macports.Observation{}, errors.New("fetch observation session is unavailable")
		}
		observations, err := input.observe.Observe(ctx, input.data, []model.Platform{input.before.Platform}, true, true)
		if err != nil {
			return macports.Observation{}, err
		}
		return observations[0], nil
	})
	if err != nil {
		return nil, err
	}
	switch decision.Kind {
	case observe.FetchLeft:
		return nil, errProbeInconclusive.Because("%s", decision.Reason)
	case observe.FetchRefused:
		return nil, decision.Err
	case observe.FetchInconclusive:
		return nil, errProbeInconclusive.Because("%s", decision.Reason)
	case observe.FetchNoPlan:
		return nil, ErrUnsupported.Because("%w", decision.Err)
	}
	return decision.Plan, nil
}

// downloadsOf are the shipped archives' downloads.
func downloadsOf(shipped []distfetch.Shipped) []distfetch.Download {
	downloads := make([]distfetch.Download, len(shipped))
	for i, archive := range shipped {
		downloads[i] = archive.Download
	}
	return downloads
}

// planSourceOnCurrent carries out --source releases or --source archive
// on a port already at its release, at the version it is at: the source
// moves, its checksums are made for the new archive, and the version and
// revision stay; or it is refused with its reason. A Portfile that fetches
// that way already is satisfied, and changes nothing (#121, #195).
func (s *Service) planSourceOnCurrent(ctx context.Context, request Request, input *sourceInput) (archivePlan, error) {
	release := request.Release
	family, err := input.familySnapshot(ctx, s.Ports)
	if err != nil {
		return archivePlan{}, err
	}
	satisfied := archivePlan{result: Result{Target: input.target, Release: release, SourceOutcome: model.ActionSatisfied, Fidelity: []Fidelity{{Before: family, After: family}}}}
	if input.info.GitFetched() && request.SourceMode == SourceArchive {
		return archivePlan{}, ErrUnsupported.Because("--source archive: GitHub's archive of the tag leaves out the Git submodules %s clones", input.target.Name)
	}
	contents, after := input.data, family
	var choice chosenSource
	if request.SourceMode == SourceArchive && input.info.Options["github.tarball_from"] == "tarball" && !input.info.GitFetched() {
		moved, ok := portfile.ArchiveFromTarball(contents, input.target.Subport)
		if !ok {
			return archivePlan{}, ErrUnsupported.Because("--source archive: github.tarball_from tarball isn't set by one literal command")
		}
		evaluated, err := s.evaluateEdit(ctx, input, moved)
		if err != nil {
			if ctx.Err() != nil {
				return archivePlan{}, ctx.Err()
			}
			return archivePlan{}, ErrUnsupported.Because("--source archive: the Portfile couldn't be evaluated fetching GitHub's archive (%v)", err)
		}
		if evaluated.after.Ports[input.target.Name].Options["github.tarball_from"] != "archive" {
			return archivePlan{}, ErrUnsupported.Because("--source archive: the Portfile didn't evaluate github.tarball_from archive")
		}
		choice = chosenSource{contents: moved, after: evaluated.after, moved: &MovedSource{From: "tarball", To: "archive"}}
	} else {
		choice, err = s.chooseSource(ctx, request, input, contents, after)
		if err != nil {
			return archivePlan{}, err
		}
	}
	if choice.moved == nil {
		return satisfied, nil
	}
	input.sourceMoved = choice.moved
	observed, err := s.planObservedArchives(ctx, request, input, choice.contents)
	result := Result{Target: input.target, Release: release, SourceMoved: choice.moved, SourceOutcome: model.ActionDone}
	result.report(scopedVersion(request, input, family, choice.after, input.target.Name, choice.after.Ports[input.target.Name].Options["checksums"]))
	if observed != nil {
		result.Left = observed.left
	}
	return archivePlan{result: result, contents: choice.contents, observed: observed, fetched: choice.fetched, subject: choice.moved.Words()}, err
}
