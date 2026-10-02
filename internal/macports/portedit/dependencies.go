package portedit

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

func (s *Service) prepareVersion(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	result, err := s.prepareNewVersion(ctx, request, input)
	if err != nil {
		return result, err
	}
	return result, s.dropStealthDistSubdir(ctx, input, &result)
}

func (s *Service) prepareNewVersion(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	if request.Release != nil && request.Release.NoUpdate {
		return s.prepareArchiveVersion(ctx, request, input)
	}
	plan, err := inspectDependencies(input)
	if err != nil {
		return Result{}, err
	}
	if plan == nil {
		return s.prepareArchiveVersion(ctx, request, input)
	}
	executable, err := s.DependencyTools.Resolve(plan.Kind)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s: %w", ErrUnsupported, input.target.Name, err)
	}
	return s.prepareDependencyVersion(ctx, request, input, plan, executable)
}

func inspectDependencies(input *sourceInput) (*depblock.Plan, error) {
	for _, key := range []string{depblock.Go, depblock.Cargo, depblock.CargoGit, "cargo.update", "cargo.dir", "cargo.offline_cmd"} {
		if input.info.OptionErrors[key] != "" {
			return nil, fmt.Errorf("%w: cannot evaluate %s", ErrUnsupported, key)
		}
	}
	plan, err := depblock.Inspect(input.data, input.info.Options)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUnsupported, input.target.Name, err)
	}
	if plan != nil {
		if err := dependencyPatches(input, plan.Kind); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// checkCargoUpdate refuses a Cargo port whose cargo.update can't be read
// as Tcl reads a boolean. One that's on is taken, the person's word (D19,
// 2026-10-02): its crates are regenerated from the Cargo.lock the source
// ships, which a source without one has none of, and refuses, and
// MacPorts re-resolves offline against them; dockhand never sets or
// clears the option.
func checkCargoUpdate(info macports.PortInfo) error {
	if _, err := info.Bool("cargo.update"); err != nil {
		return fmt.Errorf("%w: cargo.update can't be read as yes or no", ErrUnsupported)
	}
	return nil
}

func dependencyPatches(input *sourceInput, kind string) error {
	if kind == depblock.Cargo {
		if err := checkCargoUpdate(input.info); err != nil {
			return err
		}
	}
	names := []string{"Cargo.lock", "Cargo.toml"}
	if kind == depblock.Go {
		names = []string{"go.mod", "go.sum", "go.work"}
	}
	changed := func(data string) bool {
		for _, name := range names {
			if strings.Contains(data, name) {
				return true
			}
		}
		return false
	}
	script, errs := syntax.Parse(input.data)
	if len(errs) > 0 {
		return fmt.Errorf("%w: invalid Portfile", ErrUnsupported)
	}
	var checkHooks func(*syntax.Script) error
	checkHooks = func(script *syntax.Script) error {
		for _, item := range script.Items {
			cmd, ok := item.(syntax.Command)
			if !ok {
				continue
			}
			name, _ := cmd.Name(input.data)
			if (name == "pre-patch" || name == "post-patch" || name == "patch" || name == "post-extract" || name == "pre-configure") && changed(cmd.Span.Text(input.data)) {
				return fmt.Errorf("%w: %s edits a dependency manifest; regenerate manually", ErrUnsupported, name)
			}
			for _, word := range cmd.Words[1:] {
				if body, ok := word.BracedScript(input.data); ok {
					if err := checkHooks(body); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if err := checkHooks(script); err != nil {
		return err
	}
	// The info was evaluated in a projection, the workspace or an overlay
	// for a derived baseline, and its filespath names that projection: the
	// port directory it is checked against is the same projection's.
	portdir := input.portdirIn(input.before.Root)
	if err := distfetch.LocalPatches(input.info, portdir); err != nil {
		return err
	}
	patches, _ := syntax.ListValues(input.info.Options["patchfiles"])
	root := input.info.Options["filespath"]
	if root == "" {
		root = filepath.Join(portdir, "files")
	}
	for _, name := range patches {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		if changed(string(data)) {
			return fmt.Errorf("%w: patch %s edits dependency manifests; regenerate manually", ErrUnsupported, name)
		}
	}
	return nil
}

// dependencyBase is the port with its dependency declarations stripped, as
// the current version's source, with that source's archives, all of them,
// and the ones that may hold the dependency manifest.
func (s *Service) dependencyBase(ctx context.Context, request Request, input *sourceInput, plan *depblock.Plan) (*sourceInput, []distfetch.Source, []distfetch.Source, error) {
	stripped, err := plan.Strip(input.data)
	if err != nil {
		return nil, nil, nil, err
	}
	evaluated, err := s.evaluateEdit(ctx, input, stripped)
	if err != nil {
		return nil, nil, nil, err
	}
	base := input.derive(stripped, evaluated.after)

	// The stripped baseline was evaluated in an overlay, and its paths name
	// that overlay; the sources policy checks them against its port
	// directory there.
	all, err := distfetch.Sources(base.info, base.portdirIn(base.before.Root))
	if err != nil {
		return nil, nil, nil, err
	}
	candidates, err := dependencySources(base.info, all)
	if err != nil {
		return nil, nil, nil, err
	}
	return base, all, candidates, nil
}

func (s *Service) prepareDependencyVersion(ctx context.Context, request Request, input *sourceInput, plan *depblock.Plan, executable string) (Result, error) {
	base, all, sources, err := s.dependencyBase(ctx, request, input, plan)
	if err != nil {
		return Result{}, err
	}
	// Complete local candidate/context checks before any source transfer or helper.
	archivePlan, err := s.planArchiveVersion(ctx, request, base)
	if err != nil {
		return archivePlan.result, err
	}
	stripped := base.data
	baseRequest := request
	directory, err := scratch.Dir("dependencies-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(directory)
	store := s.Archives.Store(directory)
	// An update that keeps archives to compare keeps the current version's,
	// every one, beside the new ones, as an archive update does; the
	// manifest is read from among them.
	fetch, kept := sources, request.KeepArchives != ""
	if kept {
		store, fetch = s.Archives.Store(request.KeepArchives), all
	}
	cargoUpdate, _ := input.info.Bool("cargo.update")
	oldInput, previous, previousProblem, err := originalDependencySource(ctx, store, base.info, fetch, sources, plan, kept)
	if err = unlocked(err, cargoUpdate, input.info.Version); err != nil {
		return Result{}, err
	}
	progress.VerboseReport(ctx, "Checking existing %s against the original source", plan.Kind)
	old, err := depblock.Generate(ctx, plan.Kind, executable, oldInput)
	err = unlocked(err, cargoUpdate, input.info.Version)
	// A module that moved can't be generated at its old version under its
	// new path: the existing block isn't checked, and that's said, rather
	// than the update refused (pomo's, field testing, 2026-10-02).
	var moved *depblock.ModuleMoved
	unchecked := ""
	switch {
	case errors.As(err, &moved):
		unchecked = fmt.Sprintf("The module moved from %s to %s, so the existing %s wasn't checked against %s's source; it's regenerated whole.", moved.From, moved.To, plan.Kind, input.info.Version)
	case err != nil:
		return Result{}, err
	}
	old = old.KeepingDeclared(plan.Values[depblock.CargoGit])
	// Preserve maintained overrides by refusing to overwrite declarations that differ
	// from what the original source and generator describe.
	// An empty block holds no maintained override, so there's nothing to
	// check it against (fillEmptyBlock).
	oldValues := maps.Clone(plan.Values)
	if unchecked == "" && !plan.Empty() {
		if oldValues, _, err = s.gitCrateChecksums(ctx, request, input, plan, stripped, old); err != nil {
			return Result{}, err
		}
	}
	// A registry crate the Portfile pins at another version than the lock
	// is an override, kept until the new lock moves past it; any other
	// difference is refused, named (termusic's, field testing, 2026-10-02).
	var overrides []depblock.Difference
	for _, name := range slices.Sorted(maps.Keys(plan.Values)) {
		differences, err := depblock.Differences(name, plan.Values[name], oldValues[name])
		if err != nil {
			return Result{}, fmt.Errorf("%w: existing %s differs from the original manifest/helper output; preserve these overrides with manual preparation", ErrUnsupported, name)
		}
		var named []string
		for _, difference := range differences {
			if name == depblock.Cargo && difference.Override() {
				overrides = append(overrides, difference)
				continue
			}
			named = append(named, difference.Name)
		}
		if len(named) > 0 {
			return Result{}, fmt.Errorf("%w: existing %s differs from the original manifest/helper output for %s; preserve these overrides with manual preparation", ErrUnsupported, name, strings.Join(named, ", "))
		}
	}
	result, err := s.applyArchivePlan(ctx, baseRequest, base, archivePlan, store)
	if err != nil {
		return Result{}, err
	}
	next := result.Prepared.Ports[input.target.Name]
	nextSources, err := distfetch.Sources(next, base.portdirIn(result.Prepared.Root))
	if err != nil {
		return Result{}, err
	}
	nextSources, err = dependencySources(next, nextSources)
	if err != nil {
		return Result{}, err
	}
	nextInput, err := selectDependencySource(ctx, next, nextSources, result.Downloads, plan)
	if err = unlocked(err, cargoUpdate, request.Release.Version); err != nil {
		return Result{}, err
	}
	progress.VerboseReport(ctx, "Regenerating %s for %s", plan.Kind, cmp.Or(nextInput.Tag, request.Release.Tag))
	generated, err := depblock.Generate(ctx, plan.Kind, executable, nextInput)
	if err = unlocked(err, cargoUpdate, request.Release.Version); err != nil {
		return Result{}, err
	}
	generated = generated.KeepingDeclared(plan.Values[depblock.CargoGit])
	if len(generated.Online) > 0 {
		progress.Report(ctx, "Leaving %d Git-pinned crates to Cargo's online resolution at build time because cargo.offline_cmd is empty: %s", len(generated.Online), depblock.GitSummary(generated.Online))
	}
	values, gitDownloads, err := s.gitCrateChecksums(ctx, request, input, plan, result.Files[0].After, generated)
	if err != nil {
		return Result{}, err
	}
	var dropped []Override
	for _, override := range overrides {
		locked, past := override.MovedPast(values[depblock.Cargo])
		if !past {
			return Result{}, fmt.Errorf("%w: existing %s pins %s %s over the lock's %s, and %s's lock doesn't move past it; preserve this override with manual preparation", ErrUnsupported, depblock.Cargo, override.Name, override.Declared, override.Generated, request.Release.Version)
		}
		dropped = append(dropped, Override{Name: override.Name, Pinned: override.Declared, Was: override.Generated, Locked: locked})
	}
	contents, err := plan.Apply(result.Files[0].After, values)
	if err != nil {
		return Result{}, err
	}
	var regenerated []Regenerated
	for _, name := range slices.Sorted(maps.Keys(values)) {
		if count, changed, err := depblock.Entries(name, plan.Values[name], values[name]); err == nil {
			block := Regenerated{Option: name, Count: count, Changed: changed}
			if name == depblock.Cargo {
				block.Dropped = dropped
			}
			if name == plan.Kind {
				block.Unchecked = unchecked
			}
			if name == depblock.CargoGit && len(generated.Relabelled) > 0 {
				block.Notices = append(block.Notices, inertWords(generated.Relabelled))
			}
			if name == depblock.Cargo && cargoUpdate {
				block.Notices = append(block.Notices, "cargo.update is on; MacPorts re-resolves offline against these crates.")
			}
			regenerated = append(regenerated, block)
		}
	}
	evaluated, err := s.evaluateEdit(ctx, input, contents)
	if err != nil {
		return Result{}, err
	}
	after := evaluated.after
	selected := after.Ports[input.target.Name]
	if selected.Version != request.Release.Version || selected.Revision != 0 || selected.Epoch != input.info.Epoch || selected.Options["git.branch"] != request.Release.Tag {
		return Result{}, fmt.Errorf("%w: dependency regeneration changed the selected version or source", ErrFidelity)
	}
	for name, wanted := range values {
		actual, errs := syntax.ListValues(selected.Options[name])
		if len(errs) > 0 || !slices.Equal(actual, wanted) {
			return Result{}, fmt.Errorf("%w: evaluated %s differs from regenerated declarations", ErrFidelity, name)
		}
	}
	family, err := input.familySnapshot(ctx, s.Ports)
	if err != nil {
		return Result{}, err
	}
	final := Fidelity{Before: family, After: after, ExpectedChanges: []string{input.target.Name + ".version, revision, source checksums and regenerated dependencies"}}
	if len(family.Ports) != len(after.Ports) {
		return Result{}, fmt.Errorf("%w: dependency regeneration changed the port set", ErrFidelity)
	}
	// A sibling that shares the release moves with the target: its version,
	// the options that follow it, its checksums, and the regenerated block
	// it shares. Every other sibling must be untouched. The scope decides
	// which is which, and refuses an unauthorized move as the version check
	// did before the downloads.
	scope, err := fidelity.ReleaseScope(family, after, input.target.Name, request.SharedRelease)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrFidelity, err)
	}
	affected := map[string]bool{}
	for _, member := range scope.Affected {
		affected[member.Target.Name] = true
	}
	for name, old := range family.Ports {
		if name == input.target.Name {
			continue
		}
		next, ok := after.Ports[name]
		if !ok {
			return Result{}, fmt.Errorf("%w: sibling port disappeared", ErrFidelity)
		}
		before, now := fidelity.ComparablePort(old, family.Root), fidelity.ComparablePort(next, after.Root)
		if affected[name] {
			before.Version = now.Version
			for _, key := range append([]string{"checksums", depblock.Go, depblock.Cargo, depblock.CargoGit}, macports.VersionFollowers...) {
				if value, ok := now.Options[key]; ok {
					before.Options[key] = value
				} else {
					delete(before.Options, key)
				}
			}
		} else if old.Revision != next.Revision {
			final.UnexpectedChanges = append(final.UnexpectedChanges, name+".revision changed")
		}
		final.UnexpectedChanges = append(final.UnexpectedChanges, fidelity.Compare(name, before, now)...)
	}
	if len(final.UnexpectedChanges) > 0 {
		return Result{}, fmt.Errorf("%w: %v", ErrFidelity, final.UnexpectedChanges)
	}
	result.Base = request.Source
	result.Files = []portfile.Edit{evaluated.edit}
	result.report(final)
	result.Crates = gitDownloads
	result.Regenerated = regenerated
	switch {
	case kept && previousProblem != "":
		result.PreviousProblem = previousProblem
	case kept:
		result.Pairs, result.PreviousProblem = pairArchives(ctx, store, archivePlan.pairs(), previous, result.Downloads)
	}
	if err := s.raiseGoToolchain(ctx, request, input, &result); err != nil {
		return Result{}, err
	}
	if err := s.checkPatches(ctx, input, &result); err != nil {
		return Result{}, err
	}
	return result, s.dropMergedPatches(ctx, input, &result)
}

func dependencyInput(info macports.PortInfo, archive string, plan *depblock.Plan) (depblock.Input, error) {
	root := info.Options["worksrcdir"]
	if dir := info.Options["cargo.dir"]; dir != "" {
		if dir != "@worksrc@" && !strings.HasPrefix(dir, "@worksrc@/") {
			return depblock.Input{}, fmt.Errorf("%w: cargo.dir leaves the source archive", ErrUnsupported)
		}
		root = filepath.Join(root, strings.TrimPrefix(strings.TrimPrefix(dir, "@worksrc@"), "/"))
	}
	return depblock.Input{Archive: archive, Worksrcdir: filepath.ToSlash(root), Package: info.Options["go.package"], Tag: info.Options["git.branch"], Git: plan.Git}, nil
}

func (s *Service) gitCrateChecksums(ctx context.Context, request Request, input *sourceInput, plan *depblock.Plan, contents []byte, generated depblock.GeneratedBlocks) (map[string][]string, []distfetch.Download, error) {
	sums := map[string]string{}
	for _, crate := range generated.Git {
		sums[crate.Distfile()] = strings.Repeat("0", 64)
	}
	values, err := generated.WithGitChecksums(sums)
	if err != nil {
		return nil, nil, err
	}
	if len(generated.Git) == 0 {
		return values, nil, nil
	}
	provisional, err := plan.ApplyPlain(contents, values)
	if err != nil {
		return nil, nil, err
	}
	evaluated, err := s.evaluateEdit(ctx, input, provisional)
	if err != nil {
		return nil, nil, err
	}
	info := evaluated.after.Ports[input.target.Name]
	info.Options = maps.Clone(info.Options)
	for _, key := range []string{depblock.Go, depblock.Cargo, depblock.CargoGit} {
		info.Options[key] = ""
	}
	info.Options["patchfiles"] = ""
	sources, err := distfetch.Sources(info, "")
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]distfetch.Source{}
	for _, source := range sources {
		byName[source.Name] = source
	}
	var downloads []distfetch.Download
	for _, crate := range generated.Git {
		source, ok := byName[crate.Distfile()]
		if !ok {
			return nil, nil, fmt.Errorf("%w: Git crate archive missing from evaluated PortGroup", ErrFidelity)
		}
		download, err := s.Archives.Store("").Fetch(ctx, info, source)
		if err != nil {
			return nil, nil, err
		}
		sums[crate.Distfile()] = download.SHA256
		downloads = append(downloads, download)
	}
	values, err = generated.WithGitChecksums(sums)
	return values, downloads, err
}

// inertWords says which declared Git crates the lock pins other than by
// branch: the cargo PortGroup writes their label into Cargo's source
// replacement as a branch, which matches only a branch, so Cargo resolves
// them online and the declarations look unused. pgdog's, labelled master,
// are pinned by rev (field testing, 2026-10-02).
func inertWords(crates []depblock.GitCrate) string {
	var names []string
	for _, crate := range crates {
		pinned := "the default branch"
		if crate.Reference.Kind != "" {
			pinned = crate.Reference.Kind
		}
		names = append(names, crate.Name+" (pinned by "+pinned+")")
	}
	return fmt.Sprintf("cargo.crates_github declares %s under a branch, where the lock pins them otherwise; Cargo's source replacement matches only a branch, so they're resolved online and the declarations look unused.", strings.Join(names, ", "))
}

// unlocked says a cargo.update port's source that ships no Cargo.lock as
// what it is: its crates can't be regenerated, which the person's word on
// cargo.update requires (D19).
func unlocked(err error, cargoUpdate bool, version string) error {
	if cargoUpdate && errors.Is(err, macports.ErrManifestMissing) {
		return fmt.Errorf("%w: cargo.update is on and %s's source ships no Cargo.lock, so its crates can't be regenerated: %w", ErrUnsupported, version, err)
	}
	return err
}
