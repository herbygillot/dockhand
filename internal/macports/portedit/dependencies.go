package portedit

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
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
	for _, key := range []string{dependency.Go, dependency.Cargo, dependency.CargoGit, "cargo.update", "cargo.dir"} {
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
	portdir := filepath.Join(input.files.Root, filepath.Dir(input.target.Portfile))
	if err := localPatches(input.info, portdir); err != nil {
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

func (s *Service) dependencyBase(ctx context.Context, request Request, input *sourceInput, plan *dependency.Plan) (*sourceInput, []archiveSource, error) {
	stripped, err := plan.Strip(input.data)
	if err != nil {
		return nil, nil, err
	}
	_, strippedSnapshot, _, err := s.evaluateEdit(ctx, request, input, stripped)
	if err != nil {
		return nil, nil, err
	}
	baseValue := *input
	baseValue.data, baseValue.before = stripped, strippedSnapshot
	baseValue.info = strippedSnapshot.Ports[input.target.Name]
	base := &baseValue

	sources, err := downloadSources(base.info, filepath.Join(base.files.Root, filepath.Dir(base.target.Portfile)))
	if err != nil {
		return nil, nil, err
	}
	if len(sources) != 1 {
		return nil, nil, fmt.Errorf("%w: dependency regeneration requires one primary source archive", ErrUnsupported)
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
	oldArchive, err := os.CreateTemp(directory, "old-*")
	if err != nil {
		return Result{}, err
	}
	_, err = s.downloadArchive(ctx, base.info, sources[0], oldArchive)
	closeErr := oldArchive.Close()
	if err != nil {
		return Result{}, err
	}
	if closeErr != nil {
		return Result{}, closeErr
	}
	oldInput, err := dependencyInput(base.info, oldArchive.Name())
	if err != nil {
		return Result{}, err
	}
	progress.Report(ctx, "Checking existing %s against the original source", plan.Kind)
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
	worker := *s
	worker.archiveDirectory = directory
	result, err := worker.applyArchivePlan(ctx, baseRequest, base, archivePlan)
	if err != nil {
		return Result{}, err
	}
	if len(result.Downloads) != 1 {
		return Result{}, fmt.Errorf("%w: expected one downloaded dependency source", ErrUnsupported)
	}
	next := result.Fidelity[len(result.Fidelity)-1].After.Ports[input.target.Name]
	nextInput, err := dependencyInput(next, result.Downloads[0].path)
	if err != nil {
		return Result{}, err
	}
	progress.Report(ctx, "Regenerating %s for %s", plan.Kind, request.Release.Tag)
	generated, err := dependency.Generate(ctx, plan.Kind, executable, nextInput)
	if err != nil {
		return Result{}, err
	}
	values, gitDownloads, err := s.gitCrateChecksums(ctx, request, input, result.Files[0].After, generated)
	if err != nil {
		return Result{}, err
	}
	contents, err := plan.Apply(result.Files[0].After, values)
	if err != nil {
		return Result{}, err
	}
	edit, after, root, err := s.evaluateEdit(ctx, request, input, contents)
	if err != nil {
		return Result{}, err
	}
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
	final := Fidelity{Before: input.before, After: after, ExpectedChanges: []string{input.target.Name + ".version, revision, source checksums and regenerated dependencies"}}
	if len(input.before.Ports) != len(after.Ports) {
		return Result{}, fmt.Errorf("%w: dependency regeneration changed the port set", ErrFidelity)
	}
	for name, old := range input.before.Ports {
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
		final.UnexpectedChanges = append(final.UnexpectedChanges, comparePortMetadata(name, comparablePort(old, input.files.Root), comparablePort(next, root))...)
	}
	if len(final.UnexpectedChanges) > 0 {
		return Result{}, fmt.Errorf("%w: %v", ErrFidelity, final.UnexpectedChanges)
	}
	result.Base = request.Source
	result.Files = []portfile.Edit{edit}
	result.Fidelity = append(result.Fidelity, final)
	result.Downloads = append(result.Downloads, gitDownloads...)
	return result, nil
}

func dependencyInput(info macports.PortInfo, archive string) (dependency.Input, error) {
	root := info.Options["worksrcdir"]
	if dir := info.Options["cargo.dir"]; dir != "" {
		if dir != "@worksrc@" && !strings.HasPrefix(dir, "@worksrc@/") {
			return dependency.Input{}, fmt.Errorf("%w: cargo.dir leaves the source archive", ErrUnsupported)
		}
		root = filepath.Join(root, strings.TrimPrefix(strings.TrimPrefix(dir, "@worksrc@"), "/"))
	}
	return dependency.Input{Archive: archive, Worksrcdir: filepath.ToSlash(root), Package: info.Options["go.package"], Tag: info.Options["git.branch"]}, nil
}

func (s *Service) gitCrateChecksums(ctx context.Context, request Request, input *sourceInput, contents []byte, generated dependency.GeneratedBlocks) (map[string][]string, []Download, error) {
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
	_, snapshot, _, err := s.evaluateEdit(ctx, request, input, provisional)
	if err != nil {
		return nil, nil, err
	}
	info := snapshot.Ports[input.target.Name]
	info.Options = maps.Clone(info.Options)
	for _, key := range []string{dependency.Go, dependency.Cargo, dependency.CargoGit} {
		info.Options[key] = ""
	}
	info.Options["patchfiles"] = ""
	sources, err := downloadSources(info, "")
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]archiveSource{}
	for _, source := range sources {
		byName[source.Name] = source
	}
	var downloads []Download
	for _, crate := range generated.Git {
		source, ok := byName[crate.Distfile()]
		if !ok {
			return nil, nil, fmt.Errorf("%w: Git crate archive missing from evaluated PortGroup", ErrFidelity)
		}
		download, err := s.downloadArchive(ctx, info, source, nil)
		if err != nil {
			return nil, nil, err
		}
		sums[crate.Distfile()] = download.SHA256
		downloads = append(downloads, download)
	}
	values, err = generated.WithGitChecksums(sums)
	return values, downloads, err
}
