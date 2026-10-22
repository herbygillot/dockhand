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
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/textedit"
)

type archiveContext struct {
	// port is the member of the release whose archives the context binds:
	// the target, or a sibling that fetches its own at the release's
	// version, as xapian-core's bindings do.
	port    string
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
	// left is what a checksum refresh leaves as it was (ChecksumLeft).
	left []ChecksumLeft
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
	return s.bindArchivesOn(ctx, input, contents, observed, false)
}

// bindArchivesOn is bindArchives, where here says the context is this Mac
// as it is: what the evaluation read of the host is the host's, so it
// stands, where for a context modeled on another Mac it's inconclusive.
// A checksum refresh reads cpi's this way, whose qmake5 PortGroup reads
// the host's Qt paths (field testing at 43949b89).
func (s *Service) bindArchivesOn(ctx context.Context, input *sourceInput, contents []byte, observed macports.Observation, here bool) (distfiles.Binding, error) {
	port, inconclusive := observe.Tolerate(ctx, observed.Ports[input.target.Name], contents, observed.Snapshot.Root)
	if inconclusive && !here {
		return distfiles.Binding{}, errProbeInconclusive.Because("modeled context depends on host state%s", observe.HostInputs(port))
	}
	info := observed.Snapshot.Ports[input.target.Name]
	if err := distfetch.CheckPolicy(info, input.portdirIn(observed.Snapshot.Root)); err != nil {
		return distfiles.Binding{}, err
	}
	var resolve distfiles.Resolver
	if here {
		resolve = func(matches []syntax.Command) (syntax.Command, error) {
			return s.locateChecksums(ctx, input, contents, info, matches)
		}
	}
	binding, err := distfiles.BindResolving(contents, input.portfileIn(observed.Snapshot.Root), info, port, resolve)
	if err == nil && len(binding.Artifacts) == 0 {
		err = fmt.Errorf("%w; select a release subport when this is a metaport", errFetchesNothing)
	}
	return binding, err
}

// errFetchesNothing is a context where the port has no archive to
// download, as a metaport or a _select port has. It adds no archive to
// an update's plan, and an update of a port that fetches nothing anywhere
// edits its version alone; a checksum refresh has nothing to refresh.
var errFetchesNothing = ErrUnsupported.Because("no downloadable source archives")

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
		return nil, errProbeInconclusive.Because("observing baseline %v", err)
	}
	afters, err := input.observe.Observe(ctx, contents, profiles, true, false)
	if err != nil {
		return nil, errProbeInconclusive.Because("observing candidate %v", err)
	}
	// observeContext checks one context's baseline and candidate, and plans
	// what it fetches: a profile's, or a variant's on this Mac.
	// owners are the siblings that fetch their own archives at the
	// release's version, each planned as the target is, its own archives
	// fetched and its own checksums written.
	var owners []model.Target
	observeContext := func(member *sourceInput, profile model.Platform, variant string, session *observe.Session, before, after macports.Observation) error {
		old, next := before.Snapshot.Ports[member.target.Name], after.Snapshot.Ports[member.target.Name]
		if obsoleteIn(old) && obsoleteIn(next) {
			// The port is an obsolete follower in this context, with no
			// archive of its own; the context adds no requirement.
			progress.VerboseReport(ctx, "%s is obsolete on %s %s %s; no archive to cover there", member.target.Name, profile.OS, profile.Version, profile.Architecture)
			return nil
		}
		affected := old.Version != next.Version
		if next.Fetch != nil && next.Fetch.Rejected {
			progress.VerboseReport(ctx, "Preserving rejection-only fetch guard for %s %s %s; archive coverage does not establish build support", profile.OS, profile.Version, profile.Architecture)
		}
		if affected && (old.Version != input.info.Version || next.Version != request.Release.Version) {
			return ErrFidelity.Because("candidate changed an independent version on %+v", profile)
		}
		if !affected {
			if err := fidelity.Equivalent(before.Snapshot, after.Snapshot); err != nil {
				return fmt.Errorf("protected context %+v: %w", profile, err)
			}
		} else {
			report := fidelity.ScopedVersion(request.SharedRelease, before.Snapshot, after.Snapshot, member.target.Name, *request.Release, next.Options["checksums"])
			if len(report.UnexpectedChanges) > 0 {
				return ErrFidelity.Because("context %+v: %s", profile, strings.Join(report.UnexpectedChanges, "; "))
			}
		}
		// A context where the port fetches nothing, before and after,
		// adds no archive to plan.
		oldBinding, err := s.bindArchives(ctx, member, input.data, before)
		hadNothing := errors.Is(err, errFetchesNothing)
		if err != nil && !hadNothing {
			return fmt.Errorf("baseline %+v: %w", profile, err)
		}
		binding, err := s.bindArchives(ctx, member, contents, after)
		nothing := errors.Is(err, errFetchesNothing)
		if err != nil && !nothing {
			return fmt.Errorf("candidate %+v: %w", profile, err)
		}
		if hadNothing != nothing {
			return ErrFidelity.Because("candidate changed whether %s fetches anything on %+v", member.target.Name, profile)
		}
		fetches = fetches || !nothing
		if member == input {
			if _, err := s.sharedArchiveOwners(ctx, input, input.data, before, oldBinding); err != nil {
				return err
			}
			found, err := s.sharedArchiveOwners(ctx, input, contents, after, binding)
			if err != nil {
				return err
			}
			for _, owner := range found {
				if !slices.ContainsFunc(owners, func(t model.Target) bool { return t.Name == owner.Name }) {
					owners = append(owners, owner)
				}
			}
		}
		if len(oldBinding.Groups) != len(binding.Groups) || len(oldBinding.Artifacts) != len(binding.Artifacts) {
			return ErrFidelity.Because("candidate changed archive/checksum structure")
		}
		oldGroups := map[string]distfiles.Group{}
		for _, group := range oldBinding.Groups {
			oldGroups[group.ID()] = group
		}
		for _, group := range binding.Groups {
			previous, ok := oldGroups[group.ID()]
			if !ok || len(previous.Values) != len(group.Values) {
				return ErrFidelity.Because("candidate activated different checksum declarations")
			}
			for algorithm, value := range group.Values {
				if previous.Values[algorithm].Value != value.Value {
					return ErrFidelity.Because("candidate changed checksum literals")
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
				return ErrFidelity.Because("candidate changed archive ownership")
			}
			if slices.Equal(previous.URLs, artifact.URLs) {
				protected[id] = true
				if previous.Name != artifact.Name {
					return ErrFidelity.Because("filename changed without fetch location")
				}
				continue
			}
			if !affected {
				return ErrFidelity.Because("protected artifact location changed")
			}
			changed[id] = true
			coverage.download(artifact, next)
			if !paired[id] {
				paired[id] = true
				plan.pairs = append(plan.pairs, archivePair{previous: previous, info: old, port: next, next: artifact.Name})
			}
		}
		plan.contexts = append(plan.contexts, archiveContext{port: member.target.Name, profile: profile, variant: variant, session: session, before: before.Snapshot, after: after.Snapshot, binding: binding, affected: affected})
		return nil
	}
	for i, profile := range profiles {
		progress.DebugReport(ctx, "Checking archive context %s %s %s", profile.OS, profile.Version, profile.Architecture)
		if err := observeContext(input, profile, "", input.observe, befores[i], afters[i]); err != nil {
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
			return nil, ErrUnsupported.Because("+%s declares archives of its own, and couldn't be observed: %v", variant, err)
		}
		after, err := session.Observe(ctx, contents, native, true, false)
		if err != nil {
			return nil, ErrUnsupported.Because("+%s declares archives of its own, and couldn't be observed with the new version: %v", variant, err)
		}
		if err := observeContext(input, input.before.Platform, variant, session, before[0], after[0]); err != nil {
			return nil, fmt.Errorf("+%s: %w", variant, err)
		}
	}
	// Each sibling of its own source is planned in every profile as the
	// target was, its archives and checksums its own (part 3's strict
	// fidelity: xapian-core).
	for _, owner := range owners {
		member := input.forMember(owner)
		for i, profile := range profiles {
			if err := observeContext(member, profile, "", input.observe, befores[i], afters[i]); err != nil {
				return nil, fmt.Errorf("%s: %w", owner.Name, err)
			}
		}
	}
	if ids := coverage.uncovered(); len(ids) > 0 {
		return nil, errProbeInconclusive.Because("checksum declaration %s has no archive in the observed contexts", ids[0])
	}
	plan.downloads = coverage.downloads
	for id := range changed {
		if protected[id] {
			return nil, ErrFidelity.Because("checksum declaration %s is shared with a protected archive", id)
		}
	}
	// A port that fetches nothing anywhere, a metaport or a _select port,
	// downloads nothing, and its update edits its version alone: one a
	// livecheck reads, or one named, as terraform's obsolete stub was
	// (field testing, 2026-10-02, the person's word). One whose version is
	// MacPorts' own has no release to find, so only a named one reaches
	// here.
	if len(plan.downloads) == 0 && fetches {
		return nil, ErrUnsupported.Because("version edit did not change the download source")
	}
	return plan, nil
}

// locateChecksums is the probe's answer to a checksums declaration whose
// text several commands share, as LyX's two blocks of the same words in
// two branches: each match's first digest is changed, and the one whose
// change moves the target's evaluated checksums is the one that ran. One
// that none, or more than one, moves stays ambiguous, as the text said
// (the roadmap's v0.5.0 part 3, step 2). It evaluates this Mac's context.
func (s *Service) locateChecksums(ctx context.Context, input *sourceInput, contents []byte, info macports.PortInfo, matches []syntax.Command) (syntax.Command, error) {
	var moved []syntax.Command
	for _, cmd := range matches {
		span, ok := firstDigest(contents, cmd)
		if !ok {
			continue
		}
		digest := span.Text(contents)
		changed := digest[:len(digest)-1] + map[bool]string{true: "1", false: "0"}[digest[len(digest)-1] == '0']
		probe, err := textedit.Apply(contents, []textedit.Edit{{Span: span, New: []byte(changed)}})
		if err != nil {
			continue
		}
		evaluated, err := s.evaluateCandidate(ctx, input, probe)
		if err != nil {
			if ctx.Err() != nil {
				return syntax.Command{}, ctx.Err()
			}
			continue
		}
		if after, ok := evaluated.after.Ports[input.target.Name]; ok && after.Options["checksums"] != info.Options["checksums"] {
			moved = append(moved, cmd)
		}
	}
	if len(moved) != 1 {
		return syntax.Command{}, &portfile.AmbiguousDeclaration{Matches: matches}
	}
	return moved[0], nil
}

// firstDigest is the first checksum value a checksums command writes
// literally, after its algorithm.
func firstDigest(src []byte, cmd syntax.Command) (textedit.Span, bool) {
	for i := 1; i+1 < len(cmd.Words); i++ {
		kind, ok := cmd.Words[i].Literal(src)
		if !ok || !portfile.IsChecksumKind(kind) {
			continue
		}
		value, ok := cmd.Words[i+1].Literal(src)
		if ok && len(value) >= 8 && !cmd.Words[i+1].Expand {
			return cmd.Words[i+1].Span, true
		}
	}
	return textedit.Span{}, false
}
