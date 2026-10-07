package portedit

import (
	"context"
	"errors"
	"fmt"
	"slices"

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
			return depblock.Input{}, fmt.Errorf("%w: manifest candidate %s needs exactly one available source archive", ErrUnsupported, source.Name)
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
			continue
		}
		if err != nil {
			return depblock.Input{}, fmt.Errorf("%w: inspect dependency source %s: %w", ErrUnsupported, source.Name, err)
		}
		if selected != nil {
			return depblock.Input{}, fmt.Errorf("%w: multiple extracted archives contain the dependency manifest; select its source manually", ErrUnsupported)
		}
		selected = &input
	}
	if selected == nil {
		return depblock.Input{}, fmt.Errorf("%w: no extracted archive contains the dependency manifest at worksrcdir (%w)", ErrUnsupported, macports.ErrManifestMissing)
	}
	return *selected, nil
}
