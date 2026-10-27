package portedit

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// prepareChecksums re-downloads every declared archive and rewrites the
// checksums it owns. With an observer, every modeled context is covered, so
// per-platform distfiles are refreshed the same way a version bump refreshes
// them; without one, the plain declaration path handles a single context.
func (s *Service) prepareChecksums(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	plan, err := vendoredSources(input)
	if err != nil {
		return Result{Base: request.Source, Target: input.target}, err
	}
	if err := staleVendored(request, input, plan); err != nil {
		return Result{Base: request.Source, Target: input.target}, err
	}
	var result Result
	if plan != nil {
		result, err = s.refreshVendoredChecksums(ctx, request, input, plan)
	} else {
		result, err = s.refreshChecksums(ctx, request, input)
	}
	if err != nil {
		return result, err
	}
	return result, s.stealthUpdate(ctx, request, input, &result)
}

// staleVendored refuses a checksum refresh of a port whose version was
// edited by hand since its base while its crates or Go modules are still
// the base's: they're the old version's, which the refresh doesn't touch,
// and nothing said so (v0.4.0's known issues). It changes nothing, and
// says the way on.
func staleVendored(request Request, input *sourceInput, plan *depblock.Plan) error {
	asked := request.Stealth
	if plan == nil || asked == nil || asked.Base == nil || !slices.Contains(asked.Changed, input.target.Portfile) {
		return nil
	}
	base := asked.Base
	if base.Version == input.info.Version || base.Options[plan.Kind] != input.info.Options[plan.Kind] {
		return nil
	}
	name := input.target.Name
	return refusal.New(refusal.Unsupported, "%s's version was edited from %s to %s, and its %s are still %s's, which checksums doesn't refresh; nothing was changed. Undo the version edit, then dockhand update %s %s regenerates them",
		name, base.Version, input.info.Version, plan.Kind, base.Version, name, input.info.Version)
}

// vendoredSources are a port's crates or Go modules, as its Portfile
// declares them (dependency.Inspect), or nil for a port with none.
func vendoredSources(input *sourceInput) (*depblock.Plan, error) {
	plan, err := depblock.Declared(input.data, input.info)
	if err != nil {
		return nil, ErrUnsupported.Because("%s: %w", input.target.Name, err)
	}
	return plan, nil
}

// refreshVendoredChecksums refreshes the checksums of a port's own archives
// where its Portfile declares its crates or Go modules too. Those
// declarations are Cargo.lock's or go.sum's, with the checksums they carry,
// and MacPorts' fetch of them isn't one dockhand checks (CheckPolicy), so
// the refresh runs on the Portfile with them set aside, as an update's
// archives are computed (dependencyBase). Setting them aside leaves each
// command where it was, empty, so they go back where they were, byte for
// byte; the port then reads its refreshed checksums followed by the ones
// the declarations append, as it read the old ones, and nothing else of it
// changes.
func (s *Service) refreshVendoredChecksums(ctx context.Context, request Request, input *sourceInput, plan *depblock.Plan) (Result, error) {
	stripped, err := plan.Strip(input.data)
	if err != nil {
		return Result{Base: request.Source, Target: input.target}, err
	}
	evaluated, err := s.evaluateEdit(ctx, input, stripped)
	if err != nil {
		return Result{Base: request.Source, Target: input.target}, err
	}
	base := input.derive(stripped, evaluated.after)
	// An empty block is written from the source, which is kept to read
	// until it is.
	store := s.Archives.Store("")
	if plan.Empty() {
		directory, err := scratch.Dir("vendors-")
		if err != nil {
			return Result{Base: request.Source, Target: input.target}, err
		}
		defer os.RemoveAll(directory)
		store = s.Archives.Store(directory)
	}
	result, err := s.refreshChecksumsInto(ctx, request, base, store)
	if err != nil || len(result.Files) == 0 && !plan.Empty() {
		return result, err
	}
	if plan.Empty() {
		refreshed := input.data
		if len(result.Files) > 0 {
			refreshed = result.portfileAfter()
		}
		return s.fillEmptyBlock(ctx, request, input, base, plan, result, refreshed)
	}
	refreshed := result.portfileAfter()
	contents, err := plan.Apply(refreshed, plan.Values)
	if err != nil {
		return result, err
	}
	name := input.target.Name
	before, _ := syntax.ListValues(input.info.Options["checksums"])
	own, _ := syntax.ListValues(base.info.Options["checksums"])
	if len(before) < len(own) || !slices.Equal(before[:len(own)], own) {
		return result, ErrUnsupported.Because("%s lists %s checksums before its source-archive checksums; cannot distinguish the two groups", name, plan.Kind)
	}
	now, _ := syntax.ListValues(result.Prepared.Ports[name].Options["checksums"])
	final, err := s.evaluateEdit(ctx, input, contents)
	if err != nil {
		return result, err
	}
	report := fidelity.ScopedChecksums(input.scope, input.before, final.after, name, strings.Join(append(now, before[len(own):]...), " "))
	if err := result.commitEdit(input, request, final.edit, report, "refresh checksums"); err != nil {
		return result, err
	}
	if input.scope != nil {
		if result.Scope, err = macports.RebindReleaseScope(input.scope, final.after); err != nil {
			return result, err
		}
	}
	return result, nil
}

// refreshChecksums re-downloads a port's archives and rewrites the
// checksums they're declared by.
func (s *Service) refreshChecksums(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	result, err := s.refreshChecksumsInto(ctx, request, input, s.Archives.Store(""))
	if undeclared := new(distfiles.Undeclared); errors.As(err, &undeclared) {
		return s.declareSubportChecksums(ctx, request, input, undeclared)
	}
	return result, err
}

// declareSubportChecksums writes a checksums declaration in a subport's
// block, where none it reads covers the archives it fetches, and refreshes
// it: a declaration of placeholders, as create writes one, filled in as a
// refresh fills any. kb-old, a subport beside kb at an older version, was
// refused, kb's declaration naming kb's archive (field testing on rc3).
// Strict fidelity holds the edit to the subport's checksums; where the
// subport has no block to write in, the refusal stands.
func (s *Service) declareSubportChecksums(ctx context.Context, request Request, input *sourceInput, undeclared *distfiles.Undeclared) (Result, error) {
	result := Result{Base: request.Source, Target: input.target}
	name := input.target.Name
	placeholders, ok := portfile.DeclareSubportChecksums(input.data, name, undeclared.Names)
	if !ok {
		return result, undeclared
	}
	evaluated, err := s.evaluateEdit(ctx, input, placeholders)
	if err != nil {
		return result, err
	}
	result, err = s.refreshChecksumsInto(ctx, request, input.derive(placeholders, evaluated.after), s.Archives.Store(""))
	if err != nil {
		return result, err
	}
	if len(result.Files) == 0 {
		return result, ErrFidelity.Because("the checksums written for %s weren't filled in", name)
	}
	final, err := s.evaluateEdit(ctx, input, result.portfileAfter())
	if err != nil {
		return result, err
	}
	written := final.after.Ports[name].Options["checksums"]
	if values, _ := syntax.ListValues(written); slices.Contains(values, "0") {
		return result, ErrFidelity.Because("the checksums written for %s weren't filled in", name)
	}
	// The port and its siblings as written, evaluated as the edit is, so
	// nothing but the subport's checksums moves: kb's stay as they were.
	baseline, err := s.evaluateEdit(ctx, input, input.data)
	if err != nil {
		return result, err
	}
	report := fidelity.ScopedChecksums(input.scope, baseline.after, final.after, name, written)
	if err := result.commitEdit(input, request, final.edit, report, "refresh checksums"); err != nil {
		return result, err
	}
	if input.scope != nil {
		if result.Scope, err = macports.RebindReleaseScope(input.scope, final.after); err != nil {
			return result, err
		}
	}
	return result, nil
}

// refreshChecksumsInto is refreshChecksums, with the archives fetched
// into a store of the caller's.
func (s *Service) refreshChecksumsInto(ctx context.Context, request Request, input *sourceInput, store *distfetch.Store) (Result, error) {
	result := Result{Base: request.Source, Target: input.target}
	observed, err := s.planObservedChecksums(ctx, request, input)
	var unlocated *distfiles.Unlocated
	if errors.As(err, &unlocated) {
		return result, s.checksumsToWrite(ctx, input, err)
	}
	if err != nil {
		return result, err
	}
	result.Left = observed.left
	for _, frame := range observed.contexts {
		result.Coverage = append(result.Coverage, ContextCoverage{Fetch: frame.after.Ports[input.target.Name].Fetch, Platform: frame.profile, Variant: frame.variant, Modeled: frame.profile != input.before.Platform})
	}
	return s.applyObservedArchives(ctx, request, input, archivePlan{result: result, contents: input.data, observed: observed, subject: "refresh checksums"}, store)
}

func (s *Service) planObservedChecksums(ctx context.Context, request Request, input *sourceInput) (*observedArchivePlan, error) {
	profiles, err := input.observe.Profiles(ctx, input.data)
	if err != nil {
		return nil, err
	}
	plan := &observedArchivePlan{}
	coverage := newArchiveCoverage()
	observations, err := input.observe.Observe(ctx, input.data, profiles, true, false)
	if err != nil {
		return nil, errProbeInconclusive.Because("observing %v", err)
	}
	observeContext := func(profile model.Platform, variant string, session *observe.Session, observed macports.Observation) error {
		if obsoleteIn(observed.Snapshot.Ports[input.target.Name]) {
			return nil
		}
		here := profile == input.before.Platform
		binding, err := s.bindArchivesOn(ctx, input, input.data, observed, here)
		// A context modeled on another Mac that the evaluation can't
		// model, as one that reads the host, is left as it is, and said;
		// this Mac's is read as it stands.
		if !here && errors.Is(err, errProbeInconclusive) {
			plan.left = append(plan.left, ChecksumLeft{Kind: LeftContext, What: contextWords(profile, variant), Reason: refusal.WordsOf(err)})
			return nil
		}
		if err != nil {
			return fmt.Errorf("context %+v: %w", profile, err)
		}
		if err := s.underPortsDeclaration(ctx, input, binding); err != nil {
			return err
		}
		// A sibling with archives of its own keeps them: a refresh is the
		// target's checksums.
		if _, err := s.sharedArchiveOwners(ctx, input, input.data, observed, binding); err != nil {
			return err
		}
		info := observed.Snapshot.Ports[input.target.Name]
		coverage.declare(info, binding.Groups)
		for _, artifact := range binding.Artifacts {
			coverage.cover(artifact)
			coverage.download(artifact, info)
		}
		plan.contexts = append(plan.contexts, archiveContext{port: input.target.Name, profile: profile, variant: variant, session: session, before: observed.Snapshot, after: observed.Snapshot, binding: binding})
		return nil
	}
	for i, profile := range profiles {
		progress.DebugReport(ctx, "Checking archive context %s %s %s", profile.OS, profile.Version, profile.Architecture)
		if err := observeContext(profile, "", input.observe, observations[i]); err != nil {
			return nil, err
		}
	}
	// A variant's own archives are refreshed as an update writes them
	// (planObservedArchives).
	for _, variant := range portfile.ArchiveVariants(input.data) {
		progress.DebugReport(ctx, "Checking archive context +%s", variant)
		session := input.observe.WithVariants(map[string]bool{variant: true})
		observed, err := session.Observe(ctx, input.data, []model.Platform{input.before.Platform}, true, false)
		if err != nil {
			return nil, ErrUnsupported.Because("+%s declares archives of its own, and couldn't be observed: %v", variant, err)
		}
		if err := observeContext(input.before.Platform, variant, session, observed[0]); err != nil {
			return nil, fmt.Errorf("+%s: %w", variant, err)
		}
	}
	// A declaration no context it could read fetches stays as written, and
	// is said; one refreshed is enough to go on.
	for _, id := range coverage.uncovered() {
		what := coverage.names[id]
		if what == "" {
			what = "checksum declaration " + id
		}
		plan.left = append(plan.left, ChecksumLeft{Kind: LeftDeclaration, What: what, Reason: "no context dockhand could read fetches it"})
	}
	if len(coverage.downloads) == 0 {
		return nil, errProbeInconclusive.Because("no archive in the contexts dockhand could read; left: %s", leftWords(plan.left))
	}
	plan.downloads = coverage.downloads
	return plan, nil
}

// underPortsDeclaration is a subport's archive bound to the port's unnamed
// declaration, one distfile to one group as MacPorts reads it, where the
// archive isn't the port's: refreshing it would rewrite the port's line,
// which strict fidelity refuses, saying nothing of what to do. Where the
// port's declaration comes before the subport's block, the archive is
// Undeclared, for a declaration of the subport's own in its block, which
// MacPorts reads in the port's place; after it, the port's would be read
// over the block's, and the refusal says to move it. The bazel session's
// new subport at a version of its own was this shape (roadmap 13c3e330).
func (s *Service) underPortsDeclaration(ctx context.Context, input *sourceInput, binding distfiles.Binding) error {
	subport, port := input.target.Name, input.primary.Name
	if subport == port {
		return nil
	}
	var ports []string
	read := false
	for _, artifact := range binding.Artifacts {
		if artifact.Group.Name != "" {
			continue
		}
		if artifact.Group.Command < 0 {
			continue
		}
		place := portfile.SubportPlace(input.data, subport, artifact.Group.Command)
		if place != portfile.PlaceBeforeBlock && place != portfile.PlaceAfterBlock {
			continue
		}
		if !read {
			baseline, err := s.evaluateEdit(ctx, input, input.data)
			if err != nil {
				return err
			}
			names, _ := syntax.ListValues(baseline.after.Ports[port].Options["distfiles"])
			for _, name := range names {
				// A distfile's :tag names its site, not the file.
				if cut := strings.LastIndexByte(name, ':'); cut > 0 {
					name = name[:cut]
				}
				ports = append(ports, name)
			}
			read = true
		}
		if slices.Contains(ports, artifact.Name) {
			continue
		}
		if place == portfile.PlaceAfterBlock {
			return ErrUnsupported.Because("%s reads %s's checksums, declared after its block and so read over any of its own; move %s's checksums line above the subport %s block, then dockhand checksums %s writes its own", subport, port, port, subport, subport)
		}
		undeclared := &distfiles.Undeclared{Name: artifact.Name, Err: ErrUnsupported.Because("%s reads %s's checksums, which are for %s's archive", subport, port, port)}
		for _, fetched := range binding.Artifacts {
			undeclared.Names = append(undeclared.Names, fetched.Name)
		}
		return undeclared
	}
	return nil
}

// ChecksumLeft is what a checksum refresh left as it was, with why: a
// context it couldn't model, a declaration no readable context fetches, or
// dependency entries it couldn't write. It's a list, not prose, so the
// refresh writes the rest, and later work shortens the list.
type ChecksumLeft struct {
	Kind   LeftKind `json:"kind"`
	What   string   `json:"what"`
	Reason string   `json:"reason"`
}

// LeftKind is what a ChecksumLeft is.
type LeftKind string

const (
	// LeftContext is a platform or variant the refresh couldn't read.
	LeftContext LeftKind = "context"
	// LeftDeclaration is a checksum declaration it left as written.
	LeftDeclaration LeftKind = "declaration"
	// LeftDependencies is a dependency block's entries it couldn't write.
	LeftDependencies LeftKind = "dependencies"
)

func contextWords(profile model.Platform, variant string) string {
	if variant != "" {
		return "+" + variant
	}
	return fmt.Sprintf("%s %s %s", profile.OS, profile.Version, profile.Architecture)
}

func leftWords(left []ChecksumLeft) string {
	var words []string
	for _, l := range left {
		words = append(words, l.What+" ("+l.Reason+")")
	}
	return strings.Join(words, "; ")
}

// inertChecksumGroups names the declared checksum groups that no archive
// covers because they belong to patch files present beside the Portfile:
// an old convention declared checksums for patches that MacPorts would
// fetch only if they were absent, so with the files in place the entries
// verify nothing. They are left as written rather than refused.
func inertChecksumGroups(info macports.PortInfo, groups []distfiles.Group) map[string]bool {
	inert := map[string]bool{}
	patches, errs := syntax.ListValues(info.Options["patchfiles"])
	filespath := info.Options["filespath"]
	if len(errs) > 0 || filespath == "" {
		return inert
	}
	for _, group := range groups {
		if group.Name == "" || !slices.Contains(patches, group.Name) {
			continue
		}
		if stat, err := os.Stat(filepath.Join(filespath, group.Name)); err == nil && stat.Mode().IsRegular() {
			inert[group.ID()] = true
		}
	}
	return inert
}

// ChecksumsToWrite is a checksum refresh dockhand couldn't write, with the
// checksums the archives MacPorts' fetch plan names have now, for a person
// to write: every archive of the contexts a refresh observes, a variant's
// own included.
type ChecksumsToWrite struct {
	Checksums []portfile.Checksum
	Err       error
	// Agree is whether the checksums the port evaluates to already are
	// the archives', each archive's sha256, rmd160, and size among them,
	// for a refusal to say whether there's anything to change.
	Agree bool
}

func (c *ChecksumsToWrite) Error() string { return c.Err.Error() }
func (c *ChecksumsToWrite) Unwrap() error { return c.Err }

// checksumsToWrite fetches the archives a refresh would have written the
// checksums of, and returns refusal with them; refusal alone where they
// couldn't be had.
func (s *Service) checksumsToWrite(ctx context.Context, input *sourceInput, refusal error) error {
	profiles, err := input.observe.Profiles(ctx, input.data)
	if err != nil {
		return refusal
	}
	observations, err := input.observe.Observe(ctx, input.data, profiles, true, false)
	if err != nil {
		return refusal
	}
	for _, variant := range portfile.ArchiveVariants(input.data) {
		observed, err := input.observe.WithVariants(map[string]bool{variant: true}).Observe(ctx, input.data, []model.Platform{input.before.Platform}, true, false)
		if err != nil {
			return refusal
		}
		observations = append(observations, observed...)
	}
	var sums []portfile.Checksum
	agree := true
	for _, observed := range observations {
		info := observed.Snapshot.Ports[input.target.Name]
		declared, _ := syntax.ListValues(info.Options["checksums"])
		plan, err := observed.Ports[input.target.Name].FetchPlan()
		if err != nil {
			return refusal
		}
		for _, file := range plan {
			if slices.ContainsFunc(sums, func(sum portfile.Checksum) bool { return sum.Name == file.Name }) {
				continue
			}
			download, err := s.Archives.Store("").FetchFirst(ctx, info, file.Name, file.URLs)
			if err != nil {
				return refusal
			}
			sums = append(sums, download.Checksum)
			for _, value := range []string{download.SHA256, download.RMD160, strconv.FormatInt(download.Size, 10)} {
				if value != "" && value != "0" && !slices.Contains(declared, value) {
					agree = false
				}
			}
		}
	}
	return &ChecksumsToWrite{Checksums: sums, Err: refusal, Agree: agree && len(sums) > 0}
}

// fillEmptyBlock writes a dependency block the Portfile declares empty
// from the port's own source, with the checksums just refreshed: an empty
// block holds no maintained override to keep, and create writes a Go
// port's go.vendors so, for checksums to fill through go2port, as a Rust
// port's cargo.crates comes from its Cargo.lock (mods 1.8.1, field
// testing, batch 58). The port's version, revision, and epoch are checked
// to stay, and the block to read as written.
func (s *Service) fillEmptyBlock(ctx context.Context, request Request, input, base *sourceInput, plan *depblock.Plan, result Result, refreshed []byte) (Result, error) {
	name := input.target.Name
	executable, err := s.DependencyTools.Resolve(plan.Kind)
	if err != nil {
		return result, ErrUnsupported.Because("%s: %w", name, err)
	}
	port := result.Prepared.Ports[name]
	sources, err := distfetch.Sources(port, base.portdirIn(result.Prepared.Root))
	if err != nil {
		return result, err
	}
	if sources, err = dependencySources(port, sources); err != nil {
		return result, err
	}
	// Each archive once, though the refresh fetched it for each context.
	var downloads []distfetch.Download
	for _, download := range result.Downloads {
		if !slices.ContainsFunc(downloads, func(d distfetch.Download) bool { return d.Name == download.Name && d.Path == download.Path }) {
			downloads = append(downloads, download)
		}
	}
	in, err := selectDependencySource(ctx, port, sources, downloads, plan)
	if err != nil {
		return result, err
	}
	progress.VerboseReport(ctx, "Writing %s for %s %s", plan.Kind, name, input.info.Version)
	generated, err := depblock.Generate(ctx, plan.Kind, executable, in)
	if err != nil {
		return result, err
	}
	values, crates, err := s.gitCrateChecksums(ctx, request, input, plan, refreshed, generated)
	if err != nil {
		return result, err
	}
	contents, err := plan.Apply(refreshed, values)
	if err != nil {
		return result, err
	}
	final, err := s.evaluateEdit(ctx, input, contents)
	if err != nil {
		return result, err
	}
	selected := final.after.Ports[name]
	if selected.Version != input.info.Version || selected.Revision != input.info.Revision || selected.Epoch != input.info.Epoch {
		return result, ErrFidelity.Because("writing %s changed the port's version", plan.Kind)
	}
	var regenerated []Regenerated
	for _, option := range slices.Sorted(maps.Keys(values)) {
		actual, errs := syntax.ListValues(selected.Options[option])
		if len(errs) > 0 || !slices.Equal(actual, values[option]) {
			return result, ErrFidelity.Because("evaluated %s differs from what was written", option)
		}
		if count, changed, err := depblock.Entries(option, nil, values[option]); err == nil {
			regenerated = append(regenerated, Regenerated{Option: option, Count: count, Changed: changed})
		}
	}
	report := Fidelity{Before: input.before, After: final.after, ExpectedChanges: []string{name + ".checksums and the " + plan.Kind + " written from its source"}}
	if err := result.commitEdit(input, request, final.edit, report, "refresh checksums"); err != nil {
		return result, err
	}
	result.Regenerated, result.Crates = regenerated, crates
	return result, nil
}
