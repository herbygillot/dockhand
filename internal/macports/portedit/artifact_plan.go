package portedit

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
)

type archiveContext struct {
	profile model.Platform
	// variant is the variant asked for in a variant's context, observed
	// with session; empty for a profile's, observed as the input's.
	variant       string
	session       *observe.Session
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
	// pairs are each archive the update replaces beside its replacement,
	// once for each checksum declaration they share, whichever contexts
	// fetch them.
	pairs []archivePair
}

// archivePair is an archive the update replaces, as the context that
// fetches it declares it, and the name of the archive that replaces it
// there: gh's source tarball on one macOS, and its prebuilt zip on older
// ones, each pair its own.
type archivePair struct {
	previous distfiles.Artifact
	// info and port are the port as the context that fetches the archives
	// evaluates it, before the edit and after.
	info, port macports.PortInfo
	next       string
}

func (s *Service) bindArchives(ctx context.Context, input *sourceInput, contents []byte, observed macports.Observation) (distfiles.Binding, error) {
	port, inconclusive := observe.Tolerate(ctx, observed.Ports[input.target.Name], contents, observed.Snapshot.Root)
	if inconclusive {
		return distfiles.Binding{}, fmt.Errorf("%w: modeled context depends on host state%s", errProbeInconclusive, observe.HostInputs(port))
	}
	info := observed.Snapshot.Ports[input.target.Name]
	if err := distfetch.CheckPolicy(info, input.portdirIn(observed.Snapshot.Root)); err != nil {
		return distfiles.Binding{}, err
	}
	binding, err := distfiles.Bind(contents, input.portfileIn(observed.Snapshot.Root), info, port)
	if err == nil && len(binding.Artifacts) == 0 {
		err = fmt.Errorf("%w; select a release subport when this is a metaport", errFetchesNothing)
	}
	return binding, err
}

// errFetchesNothing is a context where the port has no archive to
// download, as a metaport or a _select port has. It adds no archive to
// an update's plan, and an update of a port that fetches nothing anywhere
// edits its version alone; a checksum refresh has nothing to refresh.
var errFetchesNothing = fmt.Errorf("%w: no downloadable source archives", ErrUnsupported)

func (s *Service) planObservedArchives(ctx context.Context, request Request, input *sourceInput, contents []byte) (*observedArchivePlan, error) {
	profiles, err := input.observe.Profiles(ctx, contents)
	if err != nil {
		return nil, err
	}
	plan := &observedArchivePlan{}
	coverage := newArchiveCoverage()
	protected := map[string]bool{}
	changed := map[string]bool{}
	paired := map[string]bool{}
	progress.DebugReport(ctx, "Observing %d archive contexts", len(profiles))
	// fetches is whether the port fetches anything in any context.
	fetches := false
	befores, err := input.observe.Observe(ctx, input.data, profiles, true, false)
	if err != nil {
		return nil, fmt.Errorf("%w: observing baseline %v", errProbeInconclusive, err)
	}
	afters, err := input.observe.Observe(ctx, contents, profiles, true, false)
	if err != nil {
		return nil, fmt.Errorf("%w: observing candidate %v", errProbeInconclusive, err)
	}
	// observeContext checks one context's baseline and candidate, and plans
	// what it fetches: a profile's, or a variant's on this Mac.
	observeContext := func(profile model.Platform, variant string, session *observe.Session, before, after macports.Observation) error {
		old, next := before.Snapshot.Ports[input.target.Name], after.Snapshot.Ports[input.target.Name]
		if obsoleteIn(old) && obsoleteIn(next) {
			// The port is an obsolete follower in this context, with no
			// archive of its own; the context adds no requirement.
			progress.VerboseReport(ctx, "%s is obsolete on %s %s %s; no archive to cover there", input.target.Name, profile.OS, profile.Version, profile.Architecture)
			return nil
		}
		affected := old.Version != next.Version
		if next.Fetch != nil && next.Fetch.Rejected {
			progress.VerboseReport(ctx, "Preserving rejection-only fetch guard for %s %s %s; archive coverage does not establish build support", profile.OS, profile.Version, profile.Architecture)
		}
		if affected && (old.Version != input.info.Version || next.Version != request.Release.Version) {
			return fmt.Errorf("%w: candidate changed an independent version on %+v", ErrFidelity, profile)
		}
		if !affected {
			if err := fidelity.Equivalent(before.Snapshot, after.Snapshot); err != nil {
				return fmt.Errorf("protected context %+v: %w", profile, err)
			}
		} else {
			report := fidelity.ScopedVersion(request.SharedRelease, before.Snapshot, after.Snapshot, input.target.Name, *request.Release, next.Options["checksums"])
			if len(report.UnexpectedChanges) > 0 {
				return fmt.Errorf("%w: context %+v: %s", ErrFidelity, profile, strings.Join(report.UnexpectedChanges, "; "))
			}
		}
		// A context where the port fetches nothing, before and after,
		// adds no archive to plan.
		oldBinding, err := s.bindArchives(ctx, input, input.data, before)
		hadNothing := errors.Is(err, errFetchesNothing)
		if err != nil && !hadNothing {
			return fmt.Errorf("baseline %+v: %w", profile, err)
		}
		binding, err := s.bindArchives(ctx, input, contents, after)
		nothing := errors.Is(err, errFetchesNothing)
		if err != nil && !nothing {
			return fmt.Errorf("candidate %+v: %w", profile, err)
		}
		if hadNothing != nothing {
			return fmt.Errorf("%w: candidate changed whether %s fetches anything on %+v", ErrFidelity, input.target.Name, profile)
		}
		fetches = fetches || !nothing
		if err := s.checkSharedArchiveOwners(ctx, input, input.data, before, oldBinding); err != nil {
			return err
		}
		if err := s.checkSharedArchiveOwners(ctx, input, contents, after, binding); err != nil {
			return err
		}
		if len(oldBinding.Groups) != len(binding.Groups) || len(oldBinding.Artifacts) != len(binding.Artifacts) {
			return fmt.Errorf("%w: candidate changed archive/checksum structure", ErrFidelity)
		}
		oldGroups := map[string]distfiles.Group{}
		for _, group := range oldBinding.Groups {
			oldGroups[group.ID()] = group
		}
		for _, group := range binding.Groups {
			previous, ok := oldGroups[group.ID()]
			if !ok || len(previous.Values) != len(group.Values) {
				return fmt.Errorf("%w: candidate activated different checksum declarations", ErrFidelity)
			}
			for algorithm, value := range group.Values {
				if previous.Values[algorithm].Value != value.Value {
					return fmt.Errorf("%w: candidate changed checksum literals", ErrFidelity)
				}
			}
			if !affected {
				protected[group.ID()] = true
			}
		}
		coverage.declare(next, binding.Groups)
		oldFiles := map[string]distfiles.Artifact{}
		for _, artifact := range oldBinding.Artifacts {
			oldFiles[artifact.Group.ID()] = artifact
		}
		for _, artifact := range binding.Artifacts {
			id := artifact.Group.ID()
			coverage.cover(artifact)
			previous, ok := oldFiles[id]
			if !ok {
				return fmt.Errorf("%w: candidate changed archive ownership", ErrFidelity)
			}
			if slices.Equal(previous.URLs, artifact.URLs) {
				protected[id] = true
				if previous.Name != artifact.Name {
					return fmt.Errorf("%w: filename changed without fetch location", ErrFidelity)
				}
				continue
			}
			if !affected {
				return fmt.Errorf("%w: protected artifact location changed", ErrFidelity)
			}
			changed[id] = true
			coverage.download(artifact, next)
			if !paired[id] {
				paired[id] = true
				plan.pairs = append(plan.pairs, archivePair{previous: previous, info: old, port: next, next: artifact.Name})
			}
		}
		plan.contexts = append(plan.contexts, archiveContext{profile: profile, variant: variant, session: session, before: before.Snapshot, after: after.Snapshot, binding: binding, affected: affected})
		return nil
	}
	for i, profile := range profiles {
		progress.DebugReport(ctx, "Checking archive context %s %s %s", profile.OS, profile.Version, profile.Architecture)
		if err := observeContext(profile, "", input.observe, befores[i], afters[i]); err != nil {
			return nil, err
		}
	}
	// A variant runs its declarations only where it's asked for, so one
	// whose body declares archives of its own, off by default, is observed
	// asked for, on this Mac, and its archives are planned as a context's
	// are: git-htmldocs is +doc's, and a variant nobody updated would keep
	// the old version's checksums.
	for _, variant := range portfile.ArchiveVariants(input.data) {
		progress.DebugReport(ctx, "Checking archive context +%s", variant)
		session := input.observe.WithVariants(map[string]bool{variant: true})
		native := []model.Platform{input.before.Platform}
		before, err := session.Observe(ctx, input.data, native, true, false)
		if err != nil {
			return nil, fmt.Errorf("%w: +%s declares archives of its own, and couldn't be observed: %v", ErrUnsupported, variant, err)
		}
		after, err := session.Observe(ctx, contents, native, true, false)
		if err != nil {
			return nil, fmt.Errorf("%w: +%s declares archives of its own, and couldn't be observed with the new version: %v", ErrUnsupported, variant, err)
		}
		if err := observeContext(input.before.Platform, variant, session, before[0], after[0]); err != nil {
			return nil, fmt.Errorf("+%s: %w", variant, err)
		}
	}
	if ids := coverage.uncovered(); len(ids) > 0 {
		return nil, fmt.Errorf("%w: checksum declaration %s has no archive in the observed contexts", errProbeInconclusive, ids[0])
	}
	plan.downloads = coverage.downloads
	for id := range changed {
		if protected[id] {
			return nil, fmt.Errorf("%w: checksum declaration %s is shared with a protected archive", ErrFidelity, id)
		}
	}
	// A port that fetches nothing anywhere, a metaport or a _select port,
	// downloads nothing, and its update edits its version alone: one a
	// livecheck reads, or one named, as terraform's obsolete stub was
	// (field testing, 2026-10-02, the person's word). One whose version is
	// MacPorts' own has no release to find, so only a named one reaches
	// here.
	if len(plan.downloads) == 0 && fetches {
		return nil, fmt.Errorf("%w: version edit did not change the download source", ErrUnsupported)
	}
	return plan, nil
}
