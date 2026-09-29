package portedit

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
	"github.com/herbygillot/dockhand/internal/macports/portedit/archives"
)

func dependencySources(info macports.PortInfo, sources []archives.Source) ([]archives.Source, error) {
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = s.Name
	}
	selected, err := distfiles.ManifestCandidates(info, names)
	if err != nil {
		return nil, err
	}
	var result []archives.Source
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
func originalDependencySource(ctx context.Context, store *archives.Store, info macports.PortInfo, fetch, candidates []archives.Source, plan *dependency.Plan, kept bool) (dependency.Input, []archives.Download, string, error) {
	var downloads []archives.Download
	problem := ""
	if kept {
		shipped, err := store.Shipped(ctx, info, archives.FetchPlan(fetch))
		switch {
		case ctx.Err() != nil:
			return dependency.Input{}, nil, "", ctx.Err()
		case err != nil:
			problem = err.Error()
		default:
			downloads = downloadsOf(shipped)
		}
	}
	if downloads == nil {
		for _, source := range fetch {
			download, err := store.Fetch(ctx, info, source)
			if err != nil {
				return dependency.Input{}, nil, "", err
			}
			downloads = append(downloads, download)
		}
	}
	input, err := selectDependencySource(ctx, info, candidates, downloads, plan)
	if problem != "" {
		downloads = nil
	}
	return input, downloads, problem, err
}

func selectDependencySource(ctx context.Context, info macports.PortInfo, sources []archives.Source, downloads []archives.Download, plan *dependency.Plan) (dependency.Input, error) {
	var selected *dependency.Input
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
			return dependency.Input{}, fmt.Errorf("%w: manifest candidate %s needs exactly one available source archive", ErrUnsupported, source.Name)
		}
		input, err := dependencyInput(info, filename, plan)
		if err != nil {
			return dependency.Input{}, err
		}
		rename, err := info.Bool("extract.rename")
		if err != nil {
			return dependency.Input{}, err
		}
		err = dependency.ConfirmSource(ctx, plan.Kind, input, rename)
		if errors.Is(err, macports.ErrManifestMissing) {
			continue
		}
		if err != nil {
			return dependency.Input{}, fmt.Errorf("%w: inspect dependency source %s: %w", ErrUnsupported, source.Name, err)
		}
		if selected != nil {
			return dependency.Input{}, fmt.Errorf("%w: multiple extracted archives contain the dependency manifest; select its source manually", ErrUnsupported)
		}
		selected = &input
	}
	if selected == nil {
		return dependency.Input{}, fmt.Errorf("%w: no extracted archive contains the dependency manifest at worksrcdir", ErrUnsupported)
	}
	return *selected, nil
}
