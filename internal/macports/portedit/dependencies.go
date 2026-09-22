package portedit

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/fidelity"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

func (s *Service) prepareVersion(ctx context.Context, request Request, input *sourceInput) (Result, error) {
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

func inspectDependencies(input *sourceInput) (*dependency.Plan, error) {
	for _, key := range []string{dependency.Go, dependency.Cargo, dependency.CargoGit, "cargo.update", "cargo.dir", "cargo.offline_cmd"} {
		if input.info.OptionErrors[key] != "" {
			return nil, fmt.Errorf("%w: cannot evaluate %s", ErrUnsupported, key)
		}
	}
	plan, err := dependency.Inspect(input.data, input.info.Options)
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

func dependencyPatches(input *sourceInput, kind string) error {
	if value := input.info.Options["cargo.update"]; kind == dependency.Cargo && value != "" && value != "no" && value != "false" && value != "0" {
		return fmt.Errorf("%w: cargo.update changes the upstream lockfile", ErrUnsupported)
	}
	names := []string{"Cargo.lock", "Cargo.toml"}
	if kind == dependency.Go {
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
	portdir := input.portdir()
	if err := archives.LocalPatches(input.info, portdir); err != nil {
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

func (s *Service) dependencyBase(ctx context.Context, request Request, input *sourceInput, plan *dependency.Plan) (*sourceInput, []archives.Source, error) {
	stripped, err := plan.Strip(input.data)
	if err != nil {
		return nil, nil, err
	}
	evaluated, err := s.evaluateEdit(ctx, input, stripped)
	if err != nil {
		return nil, nil, err
	}
	strippedSnapshot := evaluated.after
	baseValue := *input
	baseValue.data, baseValue.before = stripped, strippedSnapshot
	baseValue.info = strippedSnapshot.Ports[input.target.Name]
	// The stripped evaluation covers the family, and it is the baseline the
	// regenerated declarations are compared against, so the family follows
	// the rebase rather than pointing back at the Portfile as loaded.
	baseValue.family = &baseValue.before
	base := &baseValue

	sources, err := archives.Sources(base.info, base.portdir())
	if err != nil {
		return nil, nil, err
	}
	sources, err = dependencySources(base.info, sources)
	if err != nil {
		return nil, nil, err
	}
	return base, sources, nil
}

func (s *Service) prepareDependencyVersion(ctx context.Context, request Request, input *sourceInput, plan *dependency.Plan, executable string) (Result, error) {
	base, sources, err := s.dependencyBase(ctx, request, input, plan)
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
	directory, err := os.MkdirTemp("", "dockhand-dependencies-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(directory)
	store := s.Archives.Store(directory)
	oldInput, err := originalDependencySource(ctx, store, base.info, sources, plan)
	if err != nil {
		return Result{}, err
	}
	progress.VerboseReport(ctx, "Checking existing %s against the original source", plan.Kind)
	old, err := dependency.Generate(ctx, plan.Kind, executable, oldInput)
	if err != nil {
		return Result{}, err
	}
	// Preserve maintained overrides by refusing to overwrite declarations that differ
	// from what the original source and generator describe.
	oldValues, _, err := s.gitCrateChecksums(ctx, request, input, stripped, old)
	if err != nil {
		return Result{}, err
	}
	for name, values := range plan.Values {
		if !dependency.Equivalent(name, values, oldValues[name]) {
			return Result{}, fmt.Errorf("%w: existing %s differs from the original manifest/helper output; preserve these overrides with manual preparation", ErrUnsupported, name)
		}
	}
	result, err := s.applyArchivePlan(ctx, baseRequest, base, archivePlan, store)
	if err != nil {
		return Result{}, err
	}
	next := result.Prepared.Ports[input.target.Name]
	nextSources, err := archives.Sources(next, base.portdir())
	if err != nil {
		return Result{}, err
	}
	nextSources, err = dependencySources(next, nextSources)
	if err != nil {
		return Result{}, err
	}
	nextInput, err := selectDependencySource(ctx, next, nextSources, result.Downloads, plan)
	if err != nil {
		return Result{}, err
	}
	progress.VerboseReport(ctx, "Regenerating %s for %s", plan.Kind, request.Release.Tag)
	generated, err := dependency.Generate(ctx, plan.Kind, executable, nextInput)
	if err != nil {
		return Result{}, err
	}
	if len(generated.Online) > 0 {
		progress.Report(ctx, "Leaving %d Git-pinned crates to Cargo's online resolution at build time because cargo.offline_cmd is empty: %s", len(generated.Online), dependency.GitSummary(generated.Online))
	}
	values, gitDownloads, err := s.gitCrateChecksums(ctx, request, input, result.Files[0].After, generated)
	if err != nil {
		return Result{}, err
	}
	contents, err := plan.Apply(result.Files[0].After, values)
	if err != nil {
		return Result{}, err
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
	for name, old := range family.Ports {
		if name == input.target.Name {
			continue
		}
		next, ok := after.Ports[name]
		if !ok {
			return Result{}, fmt.Errorf("%w: sibling port disappeared", ErrFidelity)
		}
		if old.Revision != next.Revision {
			final.UnexpectedChanges = append(final.UnexpectedChanges, name+".revision changed")
		}
		final.UnexpectedChanges = append(final.UnexpectedChanges, fidelity.Compare(name, fidelity.ComparablePort(old, input.files.root), fidelity.ComparablePort(next, input.files.root))...)
	}
	if len(final.UnexpectedChanges) > 0 {
		return Result{}, fmt.Errorf("%w: %v", ErrFidelity, final.UnexpectedChanges)
	}
	result.Base = request.Source
	result.Files = []portfile.Edit{evaluated.edit}
	result.report(final)
	result.Downloads = append(result.Downloads, gitDownloads...)
	if err := s.raiseGoToolchain(ctx, request, input, &result); err != nil {
		return Result{}, err
	}
	if err := s.checkPatches(ctx, input, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func dependencyInput(info macports.PortInfo, archive string, plan *dependency.Plan) (dependency.Input, error) {
	root := info.Options["worksrcdir"]
	if dir := info.Options["cargo.dir"]; dir != "" {
		if dir != "@worksrc@" && !strings.HasPrefix(dir, "@worksrc@/") {
			return dependency.Input{}, fmt.Errorf("%w: cargo.dir leaves the source archive", ErrUnsupported)
		}
		root = filepath.Join(root, strings.TrimPrefix(strings.TrimPrefix(dir, "@worksrc@"), "/"))
	}
	return dependency.Input{Archive: archive, Worksrcdir: filepath.ToSlash(root), Package: info.Options["go.package"], Tag: info.Options["git.branch"], Git: plan.Git}, nil
}

func (s *Service) gitCrateChecksums(ctx context.Context, request Request, input *sourceInput, contents []byte, generated dependency.GeneratedBlocks) (map[string][]string, []archives.Download, error) {
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
	provisional, err := dependency.Apply(contents, values)
	if err != nil {
		return nil, nil, err
	}
	evaluated, err := s.evaluateEdit(ctx, input, provisional)
	if err != nil {
		return nil, nil, err
	}
	info := evaluated.after.Ports[input.target.Name]
	info.Options = maps.Clone(info.Options)
	for _, key := range []string{dependency.Go, dependency.Cargo, dependency.CargoGit} {
		info.Options[key] = ""
	}
	info.Options["patchfiles"] = ""
	sources, err := archives.Sources(info, "")
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]archives.Source{}
	for _, source := range sources {
		byName[source.Name] = source
	}
	var downloads []archives.Download
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
