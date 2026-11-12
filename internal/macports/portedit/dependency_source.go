package portedit

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/depblock"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
)

func dependencySources(info macports.PortInfo, sources []distfetch.Source) ([]distfetch.Source, error) {
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = s.Name
	}
	selected, err := distfiles.ManifestCandidates(info, names)
	if err != nil {
		return nil, err
	}
	var result []distfetch.Source
	for _, source := range sources {
		if slices.Contains(selected, source.Name) {
			result = append(result, source)
		}
	}
	return result, nil
}

// originalDependencySource fetches the current version's archives, fetch,
// and finds the dependency manifest in the one of candidates that holds
// it. Kept to compare, they are fetched as MacPorts shipped them
// (Store.Shipped), and returned. Where that fails, the manifest is read
// from what upstream serves, as it was before they were kept, and the
// problem is returned in their place: the update can't be compared with
// bytes MacPorts didn't ship.
func originalDependencySource(ctx context.Context, store *distfetch.Store, info macports.PortInfo, fetch, candidates []distfetch.Source, plan *depblock.Plan, kept bool) (depblock.Input, []distfetch.Download, error, error) {
	var downloads []distfetch.Download
	var problem error
	if kept {
		shipped, err := store.Shipped(ctx, info, distfetch.FetchPlan(fetch))
		switch {
		case ctx.Err() != nil:
			return depblock.Input{}, nil, nil, ctx.Err()
		case err != nil:
			problem = err
		default:
			downloads = downloadsOf(shipped)
		}
	}
	if downloads == nil {
		for _, source := range fetch {
			download, err := store.Fetch(ctx, info, source)
			if err != nil {
				return depblock.Input{}, nil, nil, err
			}
			downloads = append(downloads, download)
		}
	}
	input, err := selectDependencySource(ctx, info, candidates, downloads, plan)
	if problem != nil {
		downloads = nil
	}
	return input, downloads, problem, err
}

func selectDependencySource(ctx context.Context, info macports.PortInfo, sources []distfetch.Source, downloads []distfetch.Download, plan *depblock.Plan) (depblock.Input, error) {
	var selected *depblock.Input
	var archives []string
	for _, source := range sources {
		matches := 0
		var filename string
		for _, download := range downloads {
			if download.Name == source.Name {
				matches++
				filename = download.Path
			}
		}
		if matches != 1 || filename == "" {
			return depblock.Input{}, ErrUnsupported.Because("manifest candidate %s needs exactly one available source archive", source.Name)
		}
		input, err := dependencyInput(info, filename, plan)
		if err != nil {
			return depblock.Input{}, err
		}
		rename, err := info.Bool("extract.rename")
		if err != nil {
			return depblock.Input{}, err
		}
		err = depblock.ConfirmSource(ctx, plan.Kind, input, rename)
		if errors.Is(err, macports.ErrManifestMissing) {
			archives = append(archives, filename)
			continue
		}
		if err != nil {
			return depblock.Input{}, ErrUnsupported.Because("inspect dependency source %s: %w", source.Name, err)
		}
		if selected != nil {
			return depblock.Input{}, ErrUnsupported.Because("multiple extracted archives contain the dependency manifest; select its source manually")
		}
		selected = &input
	}
	if selected == nil && plan.Kind == depblock.Cargo && len(archives) > 0 {
		missing := &lockMissing{worksrcdir: info.Options["worksrcdir"]}
		for _, archive := range archives {
			files, err := depblock.CargoMembers(ctx, archive)
			if err != nil {
				return depblock.Input{}, ErrUnsupported.Because("inspect dependency source: %w", err)
			}
			missing.files.Locks = append(missing.files.Locks, files.Locks...)
			missing.files.Manifests = append(missing.files.Manifests, files.Manifests...)
		}
		return depblock.Input{}, ErrUnsupported.Because("%w", missing)
	}
	if selected == nil {
		return depblock.Input{}, ErrUnsupported.Because("no extracted archive contains the dependency manifest at worksrcdir (%w)", macports.ErrManifestMissing)
	}
	return *selected, nil
}

// lockMissing is a Cargo source none of whose candidate archives holds a
// Cargo.lock where the build at worksrcdir reads one, its own or one
// above it (depblock.CargoLock), with what they hold of Cargo's instead:
// no lock at all, as watchman's source ships none, or locks only
// elsewhere, which nothing in the Portfile points the build at, the rust
// PortGroup having no cargo.dir (#229). It is a missing manifest
// (macports.ErrManifestMissing).
type lockMissing struct {
	worksrcdir string
	files      depblock.CargoFiles
}

func (e *lockMissing) Error() string {
	if len(e.files.Locks) > 0 {
		return fmt.Sprintf("no Cargo.lock is at worksrcdir %s or above it, where the build reads one; the source holds %s, and nothing in the Portfile points the build at one, so the crate list can't be regenerated; prepare the update by hand", e.worksrcdir, strings.Join(e.files.Locks, ", "))
	}
	return "the source ships no Cargo.lock, so the crate list can't be regenerated from it; " + e.manifests()
}

func (e *lockMissing) Unwrap() error { return macports.ErrManifestMissing }

// manifests says where the source's Cargo.toml are, by their directories.
func (e *lockMissing) manifests() string {
	var dirs []string
	for _, manifest := range e.files.Manifests {
		if dir := path.Dir(manifest); !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return "it holds no Cargo.toml either"
	}
	return "Cargo.toml is in " + strings.Join(dirs, ", ")
}
