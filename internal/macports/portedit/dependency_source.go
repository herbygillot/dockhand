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

func originalDependencySource(ctx context.Context, store *archives.Store, info macports.PortInfo, sources []archives.Source, plan *dependency.Plan) (dependency.Input, error) {
	var downloads []archives.Download
	for _, source := range sources {
		download, err := store.Fetch(ctx, info, source)
		if err != nil {
			return dependency.Input{}, err
		}
		downloads = append(downloads, download)
	}
	return selectDependencySource(ctx, info, sources, downloads, plan)
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
		if errors.Is(err, dependency.ErrManifestMissing) {
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
