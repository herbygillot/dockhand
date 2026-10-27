package portedit

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
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
	if err := s.portGroupChecksums(ctx, input); err != nil {
		return Result{}, err
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
		return Result{}, ErrUnsupported.Because("%s: %w", input.target.Name, err)
	}
	return s.prepareDependencyVersion(ctx, request, input, plan, executable)
}

func inspectDependencies(input *sourceInput) (*depblock.Plan, error) {
	for _, key := range []string{depblock.Go, depblock.Cargo, depblock.CargoGit, "cargo.update", "cargo.dir", "cargo.offline_cmd"} {
		if input.info.OptionErrors[key] != "" {
			return nil, ErrUnsupported.Because("cannot evaluate %s", key)
		}
	}
	plan, err := depblock.Inspect(input.data, input.info.Options)
	if err != nil {
		return nil, ErrUnsupported.Because("%s: %w", input.target.Name, err)
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
		return ErrUnsupported.Because("cargo.update can't be read as yes or no")
	}
	return nil
}

// manifestHookRefusal is an update a manifest-editing hook holds, the block
// not being what the current version's lock gives: the way on names the
// block's own regeneration, not only the archive's checksums, as fresh's
// 700 crates needed (a user's report, editors/fresh).
func manifestHookRefusal(hook, kind, block string) error {
	tool := "cargo2port"
	if kind == depblock.Go {
		tool = "go2port"
	}
	return ErrUnsupported.Because("%s edits a dependency manifest, and the Portfile's %s isn't exactly what the current version's lock gives, so the edit may have shaped it by hand; update it by hand, regenerating %s with %s from the new version's source", hook, block, block, tool)
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
		return ErrUnsupported.Because("invalid Portfile")
	}
	var checkHooks func(*syntax.Script) error
	checkHooks = func(script *syntax.Script) error {
		for _, item := range script.Items {
			cmd, ok := item.(syntax.Command)
			if !ok {
				continue
			}
			name, _ := cmd.Name(input.data)
			// A hook that edits a manifest, as fresh's post-patch drops a
			// default feature from Cargo.toml, may have shaped the block
			// by hand: the update goes on where the block is exactly what
			// the current version's lock gives, as the maintainer's own
			// regeneration made it (a user's report, editors/fresh, at
			// 6dd7745c), and refuses otherwise.
			if (name == "pre-patch" || name == "post-patch" || name == "patch" || name == "post-extract" || name == "pre-configure") && changed(cmd.Span.Text(input.data)) && input.manifestHook == "" {
				input.manifestHook = name
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
		if changed(string(data)) && !(kind == depblock.Cargo && featuresOnly(data)) {
			return ErrUnsupported.Because("patch %s edits dependency manifests; regenerate manually", name)
		}
	}
	return nil
}

// cargoFeatures is a dependency's features as a Cargo.toml line spells
// them, its list and whether its defaults stay.
var cargoFeatures = regexp.MustCompile(`,?\s*(default-features|features)\s*=\s*(\[[^\]]*\]|true|false)`)

// featuresOnly says whether a patch changes nothing of a Cargo project's
// dependencies but the features it asks of them: it touches no
// Cargo.lock, and its Cargo.toml lines differ only in their features
// lists. Cargo resolves a lock for every feature at once, so asking other
// features of a dependency leaves the lock, and the crates, as they are
// (field testing, batch 12: jgenesis's patch asked sdl3 for
// use-pkg-config, and was refused).
func featuresOnly(patch []byte) bool {
	var removed, added []string
	cargo := false
	for _, line := range strings.Split(string(patch), "\n") {
		switch {
		case strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- "):
			name := strings.Fields(strings.TrimSpace(line[4:]))
			if len(name) == 0 {
				return false
			}
			base := path.Base(name[0])
			if base == "Cargo.lock" {
				return false
			}
			cargo = base == "Cargo.toml"
		case !cargo:
		case strings.HasPrefix(line, "-"):
			removed = append(removed, strings.TrimSpace(cargoFeatures.ReplaceAllString(line[1:], "")))
		case strings.HasPrefix(line, "+"):
			added = append(added, strings.TrimSpace(cargoFeatures.ReplaceAllString(line[1:], "")))
		}
	}
	slices.Sort(removed)
	slices.Sort(added)
	return len(removed)+len(added) > 0 && slices.Equal(removed, added)
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
		// Said as the current version's: halloy's refusal named the Git
		// pin of 2025's lock, and the update after it listed the new
		// version's, another commit (field testing, batch 11).
		return Result{}, fmt.Errorf("checking existing %s against the source for current version %s: %w", plan.Kind, base.info.Version, err)
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
	// A hook that edits the manifest is predicted to leave the block as
	// the lock gives it, which the current version's block shows where it's
	// exactly that. Where it can't be shown, the block empty or its module
	// moved, the block is regenerated and that's a finding for a look,
	// where it refused (R3-20); a block that differs from the lock refuses
	// below, as one the hook may have shaped by hand.
	hookUnchecked := ""
	if input.manifestHook != "" && (unchecked != "" || plan.Empty()) {
		hookUnchecked = fmt.Sprintf("%s edits a dependency manifest, and the Portfile's %s couldn't be checked against the current version's lock, so whether the hook shapes it isn't known; read the regenerated %s against what the hook changes before submitting", input.manifestHook, plan.Kind, plan.Kind)
	}
	// A registry crate the Portfile pins at another version than the lock
	// is an override, kept until the new lock moves past it; any other
	// difference is refused, named (termusic's, field testing, 2026-10-02).
	var overrides, keptModules []depblock.Difference
	for _, name := range slices.Sorted(maps.Keys(plan.Values)) {
		differences, err := depblock.Differences(name, plan.Values[name], oldValues[name])
		if err != nil {
			return Result{}, ErrUnsupported.Because("existing %s differs from the original manifest/helper output; preserve these overrides with manual preparation", name)
		}
		if input.manifestHook != "" && len(differences) > 0 {
			return Result{}, manifestHookRefusal(input.manifestHook, plan.Kind, name)
		}
		var named []string
		for _, difference := range differences {
			if name == depblock.Cargo && difference.Override() {
				overrides = append(overrides, difference)
				continue
			}
			// A Go module kept by hand beside go2port's, which leaves out
			// test-only ones, is kept where the new go.sum still pins it.
			if name == depblock.Go && difference.Kept {
				keptModules = append(keptModules, difference)
				continue
			}
			named = append(named, difference.Name)
		}
		if len(named) > 0 {
			return Result{}, ErrUnsupported.Because("existing %s differs from the original manifest/helper output for %s; preserve these overrides with manual preparation", name, strings.Join(named, ", "))
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
		progress.Report(ctx, "Leaving %d Git-pinned crates to Cargo's online resolution at build time, since its build resolves Git sources online: %s", len(generated.Online), depblock.GitSummary(generated.Online))
	}
	values, gitDownloads, err := s.gitCrateChecksums(ctx, request, input, plan, result.portfileAfter(), generated)
	if err != nil {
		return Result{}, err
	}
	// A module the old version's go2port output left out, which the new
	// version's has, is the generator's now, not kept, and its go.sum pin
	// isn't asked after.
	if len(keptModules) > 0 {
		modules, err := depblock.GoModules(values[depblock.Go])
		if err != nil {
			return Result{}, err
		}
		keptModules = slices.DeleteFunc(keptModules, func(kept depblock.Difference) bool { return slices.Contains(modules, kept.Name) })
	}
	var keptNames []string
	if len(keptModules) > 0 {
		gosum, _, err := depblock.Manifest(ctx, nextInput.Archive, nextInput.Worksrcdir, "go.sum")
		if err != nil {
			return Result{}, ErrUnsupported.Because("existing %s includes modules omitted by go2port; could not read go.sum for %s to verify their pinned versions: %w", depblock.Go, request.Release.Version, err)
		}
		for _, kept := range keptModules {
			if !depblock.GoSumPins(gosum, kept.Name, kept.Declared) {
				return Result{}, ErrUnsupported.Because("existing %s includes %s %s, which go2port omits, but go.sum for %s no longer pins that version; prepare the update manually to preserve the override", depblock.Go, kept.Name, kept.Declared, request.Release.Version)
			}
			keptNames = append(keptNames, kept.Name)
		}
		if values[depblock.Go], err = depblock.KeepGoModules(plan.Values[depblock.Go], values[depblock.Go], keptNames); err != nil {
			return Result{}, err
		}
	}
	var dropped []Override
	for _, override := range overrides {
		locked, past := override.MovedPast(values[depblock.Cargo])
		if !past {
			return Result{}, ErrUnsupported.Because("existing %s overrides %s to %s instead of the locked %s, and the lockfile for %s does not select a newer version than the override; prepare the update manually to preserve it", depblock.Cargo, override.Name, override.Declared, override.Generated, request.Release.Version)
		}
		dropped = append(dropped, Override{Name: override.Name, Pinned: override.Declared, Was: override.Generated, Locked: locked})
	}
	contents, err := plan.Apply(result.portfileAfter(), values)
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
			if name == depblock.Go && len(keptNames) > 0 {
				block.Notices = append(block.Notices, "Kept the modules go2port leaves out that the Portfile has, since the new go.sum still pins them: "+strings.Join(keptNames, ", ")+".")
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
	// A release read from a livecheck's listing, as garage's on its own
	// Gitea, names no tag, so the tag the PortGroup composes stands
	// (field testing, batch 11).
	tagMoved := request.Release.Tag != "" && selected.Options["git.branch"] != request.Release.Tag
	if selected.Version != request.Release.Version || selected.Revision != 0 || selected.Epoch != input.info.Epoch || tagMoved {
		return Result{}, ErrFidelity.Because("dependency regeneration changed the selected version or source: it evaluates as version %s, revision %d, epoch %d, tag %q, where the update chose %s, revision 0, epoch %d, tag %q",
			selected.Version, selected.Revision, selected.Epoch, selected.Options["git.branch"], request.Release.Version, input.info.Epoch, request.Release.Tag)
	}
	for name, wanted := range values {
		actual, errs := syntax.ListValues(selected.Options[name])
		if len(errs) > 0 || !slices.Equal(actual, wanted) {
			return Result{}, ErrFidelity.Because("evaluated %s differs from regenerated declarations", name)
		}
	}
	family, err := input.familySnapshot(ctx, s.Ports)
	if err != nil {
		return Result{}, err
	}
	if len(family.Ports) != len(after.Ports) {
		return Result{}, ErrFidelity.Because("dependency regeneration changed the port set")
	}
	// A sibling that shares the release moves with the target: what the
	// version's evaluation and the regenerated block it shares change in
	// it are theirs, as the target's are. Every other sibling must be
	// untouched. The scope decides which is which, and refuses an
	// unauthorized move as the version check did before the downloads.
	scope, err := fidelity.ReleaseScope(family, after, input.target.Name, request.SharedRelease)
	if err != nil {
		return Result{}, ErrFidelity.Because("%v", err)
	}
	// The block step predicts from the version step's own evaluation, its
	// archives' checksums written (Prepared), with only the blocks it
	// writes moved, in every member that shares the release: a collateral
	// change the block edit makes, in the target or a sibling, is
	// unexpected (architecture, 2026-10-22). Every other sibling is as the
	// family was.
	prediction := fidelity.Predict(family)
	prediction.Expected = []string{input.target.Name + ".version, revision, source checksums and regenerated dependencies"}
	for _, member := range scope.Affected {
		name := member.Target.Name
		prepared, ok := result.Prepared.Ports[name]
		if !ok {
			continue
		}
		port := fidelity.ComparablePort(prepared, result.Prepared.Root)
		next := fidelity.ComparablePort(after.Ports[name], after.Root)
		// The blocks, and what the Go and Cargo PortGroups fetch for them:
		// each module or crate is a distfile of its own, from its own
		// site, extracted apart, with its own checksums.
		for _, block := range []string{depblock.Go, depblock.Cargo, depblock.CargoGit, "distfiles", "master_sites", "extract.only", "checksums"} {
			if value, ok := next.Options[block]; ok {
				port.Options[block] = value
			} else {
				delete(port.Options, block)
			}
		}
		// What a PortGroup sets where its block lists anything, which the
		// version step, evaluating the Portfile with its blocks set aside
		// (dependencyBase), read without: where the block lists any, it's
		// the block's. The Cargo PortGroup keeps a port's archives with its
		// crates, dist_subdir cargo-crates: every Rust update that lists
		// its crates refused, "[dust.dist_subdir changed]" (v0.5.0-rc1's
		// quick stage). golang 1.0's go.vendors turns extract.rename off
		// and wraps extract.cmd, its args emptied, to extract each module
		// apart: git-credential-oauth refused, "[git-credential-oauth.
		// extract.rename changed]" (field testing, 2026-10-23).
		for _, set := range blockSettings {
			listed := false
			for _, block := range set.blocks {
				listed = listed || strings.TrimSpace(next.Options[block]) != ""
			}
			if !listed {
				continue
			}
			for _, option := range set.options {
				if value, ok := next.Options[option]; ok {
					port.Options[option] = value
				} else {
					delete(port.Options, option)
				}
			}
		}
		prediction.Ports[name] = port
	}
	final := prediction.Observe(family, after)
	if len(final.UnexpectedChanges) > 0 {
		return Result{}, ErrFidelity.Because("%v", final.UnexpectedChanges)
	}
	result.Base = request.Source
	result.Files = []portfile.Edit{evaluated.edit}
	result.report(final)
	result.Crates = gitDownloads
	result.Regenerated = regenerated
	result.ManifestHook = hookUnchecked
	switch {
	case kept && previousProblem != nil:
		result.previousFailed(previousProblem)
	case kept:
		var err error
		result.Pairs, err = pairArchives(ctx, store, archivePlan.pairs(), previous, result.Downloads)
		result.previousFailed(err)
	}
	if err := s.raiseGoToolchain(ctx, request, input, &result); err != nil {
		return Result{}, err
	}
	if err := s.checkPatches(ctx, input, &result); err != nil {
		return Result{}, err
	}
	if err := s.dropMergedPatches(ctx, input, &result); err != nil {
		return result, err
	}
	return result, s.refreshPatches(ctx, input, &result)
}

func dependencyInput(info macports.PortInfo, archive string, plan *depblock.Plan) (depblock.Input, error) {
	root := info.Options["worksrcdir"]
	if dir := info.Options["cargo.dir"]; dir != "" {
		if dir != "@worksrc@" && !strings.HasPrefix(dir, "@worksrc@/") {
			return depblock.Input{}, ErrUnsupported.Because("cargo.dir leaves the source archive")
		}
		root = filepath.Join(root, strings.TrimPrefix(strings.TrimPrefix(dir, "@worksrc@"), "/"))
	}
	return depblock.Input{Archive: archive, Worksrcdir: filepath.ToSlash(root), Package: info.Options["go.package"], Tag: info.Options["git.branch"], Git: plan.Git, Vendored: plan.Values[depblock.Go]}, nil
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
			return nil, nil, ErrFidelity.Because("Git crate archive missing from evaluated PortGroup")
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
		return ErrUnsupported.Because("cargo.update is enabled, but the source for %s contains no Cargo.lock; cannot regenerate crate dependencies: %w", version, err)
	}
	return err
}

// blockSettings are the options a PortGroup sets where a dependency block
// of its lists anything, by its handler for the block: what the block's
// evaluation changes beside it, as the block step predicts it.
var blockSettings = []struct{ blocks, options []string }{
	{[]string{depblock.Cargo, depblock.CargoGit}, []string{"dist_subdir"}},
	{[]string{depblock.Go}, []string{"extract.rename", "extract.cmd", "extract.pre_args", "extract.post_args"}},
}

// portGroupChecksums refuses an update of a port whose checksums its
// PortGroup's own code sets (portfile.InPortGroup), as crossbinutils 1.0
// sets them from its table: a new version is a row the PortGroup lacks,
// which no Portfile edit adds, so it's said as checksums says it, before
// the version is moved (the roadmap's v0.5.0 part 3, step 2). It reads
// this Mac's context.
func (s *Service) portGroupChecksums(ctx context.Context, input *sourceInput) error {
	observations, err := input.observe.Observe(ctx, input.data, []model.Platform{input.before.Platform}, true, false)
	if err != nil {
		return nil
	}
	for _, observed := range observations {
		port := observed.Ports[input.target.Name]
		for _, declaration := range port.Declarations {
			if name := strings.TrimPrefix(declaration.Command, "::"); name != "checksums" && name != "checksums-append" && name != "checksums-prepend" {
				continue
			}
			_, err := portfile.LocateDeclaration(input.data, input.portfileIn(observed.Snapshot.Root), declaration)
			if group := new(portfile.InPortGroup); errors.As(err, &group) && !portfile.WritesChecksums(input.data, declaration.Values) {
				return &distfiles.Unlocated{Reason: group}
			}
		}
	}
	return nil
}
