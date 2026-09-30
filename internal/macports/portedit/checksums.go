package portedit

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"os"
	"path/filepath"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/progress"
)

// prepareChecksums re-downloads every declared archive and rewrites the
// checksums it owns. With an observer, every modeled context is covered, so
// per-platform distfiles are refreshed the same way a version bump refreshes
// them; without one, the plain declaration path handles a single context.
func (s *Service) prepareChecksums(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	result := Result{Base: request.Source, Target: input.target}
	observed, err := s.planObservedChecksums(ctx, request, input)
	var unlocated *distfiles.Unlocated
	if errors.As(err, &unlocated) {
		return result, s.checksumsToWrite(ctx, input, err)
	}
	if err != nil {
		return result, err
	}
	for _, frame := range observed.contexts {
		result.Coverage = append(result.Coverage, ContextCoverage{Fetch: frame.after.Ports[input.target.Name].Fetch, Platform: frame.profile, Variant: frame.variant, Modeled: frame.profile != input.before.Platform})
	}
	result, err = s.applyObservedArchives(ctx, request, input, archivePlan{result: result, contents: input.data, observed: observed, subject: "refresh checksums"}, s.Archives.Store(""))
	if err != nil {
		return result, err
	}
	return result, s.stealthUpdate(ctx, request, input, &result)
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
		return nil, fmt.Errorf("%w: observing %v", errProbeInconclusive, err)
	}
	observeContext := func(profile model.Platform, variant string, session *observe.Session, observed macports.Observation) error {
		if obsoleteIn(observed.Snapshot.Ports[input.target.Name]) {
			return nil
		}
		binding, err := s.bindArchives(ctx, input, input.data, observed)
		if err != nil {
			return fmt.Errorf("context %+v: %w", profile, err)
		}
		if err := s.checkSharedArchiveOwners(ctx, input, input.data, observed, binding); err != nil {
			return err
		}
		info := observed.Snapshot.Ports[input.target.Name]
		coverage.declare(info, binding.Groups)
		for _, artifact := range binding.Artifacts {
			coverage.cover(artifact)
			coverage.download(artifact, info)
		}
		plan.contexts = append(plan.contexts, archiveContext{profile: profile, variant: variant, session: session, before: observed.Snapshot, after: observed.Snapshot, binding: binding})
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
			return nil, fmt.Errorf("%w: +%s declares archives of its own, and couldn't be observed: %v", ErrUnsupported, variant, err)
		}
		if err := observeContext(input.before.Platform, variant, session, observed[0]); err != nil {
			return nil, fmt.Errorf("+%s: %w", variant, err)
		}
	}
	if ids := coverage.uncovered(); len(ids) > 0 {
		return nil, fmt.Errorf("%w: checksum declaration %s has no archive in the observed contexts", errProbeInconclusive, ids[0])
	}
	plan.downloads = coverage.downloads
	return plan, nil
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
	for _, observed := range observations {
		info := observed.Snapshot.Ports[input.target.Name]
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
		}
	}
	return &ChecksumsToWrite{Checksums: sums, Err: refusal}
}
