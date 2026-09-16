package portedit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/distfiles"
)

func dependencySources(info macports.PortInfo, sources []archiveSource) ([]archiveSource, error) {
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = s.Name
	}
	selected, err := distfiles.ManifestCandidates(info, names)
	if err != nil {
		return nil, err
	}
	var result []archiveSource
	for _, source := range sources {
		if slices.Contains(selected, source.Name) {
			result = append(result, source)
		}
	}
	return result, nil
}

func (s *Service) originalDependencySource(ctx context.Context, info macports.PortInfo, sources []archiveSource, kind, directory string) (dependency.Input, error) {
	var downloads []Download
	for _, source := range sources {
		file, err := os.CreateTemp(directory, "original-*")
		if err != nil {
			return dependency.Input{}, err
		}
		download, err := s.downloadArchive(ctx, info, source, file)
		closeErr := file.Close()
		if err != nil {
			return dependency.Input{}, err
		}
		if closeErr != nil {
			return dependency.Input{}, closeErr
		}
		download.path = file.Name()
		downloads = append(downloads, download)
	}
	return selectDependencySource(ctx, info, sources, downloads, kind)
}

func selectDependencySource(ctx context.Context, info macports.PortInfo, sources []archiveSource, downloads []Download, kind string) (dependency.Input, error) {
	var selected *dependency.Input
	for _, source := range sources {
		matches := 0
		var filename string
		for _, download := range downloads {
			if download.Name == source.Name {
				matches++
				filename = download.path
			}
		}
		if matches != 1 || filename == "" {
			return dependency.Input{}, fmt.Errorf("%w: manifest candidate %s needs exactly one available source archive", ErrUnsupported, source.Name)
		}
		input, err := dependencyInput(info, filename)
		if err != nil {
			return dependency.Input{}, err
		}
		rename := info.Options["extract.rename"]
		err = dependency.ConfirmSource(ctx, kind, input, rename == "yes" || rename == "true" || rename == "1")
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
