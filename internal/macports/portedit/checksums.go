package portedit

import (
	"bytes"
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
)

func (s *Service) prepareChecksums(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	result := Result{Base: request.Source, Target: input.target}
	sources, err := downloadSources(input.info, input.portdir())
	if err != nil {
		return result, err
	}
	contents, checksums, downloads, err := s.archives("").refresh(ctx, input.data, input.info, sources)
	if err != nil {
		return result, err
	}
	result.Downloads = downloads
	evaluated, err := s.evaluateEdit(ctx, input, contents)
	if err != nil {
		return result, err
	}
	fidelity := checksumFidelity(input.before, evaluated.after, input.target.Name, input.files.root, checksums)
	if bytes.Equal(contents, input.data) {
		result.Fidelity = []Fidelity{fidelity}
		if len(fidelity.UnexpectedChanges) > 0 {
			return result, fmt.Errorf("%w: %v", ErrFidelity, fidelity.UnexpectedChanges)
		}
		return result, nil
	}
	return result, result.commitEdit(input, request, evaluated.edit, fidelity, "refresh checksums")
}

func checkChecksumSources(contents []byte, info macports.PortInfo, sources []archiveSource) error {
	placeholders := make([]portfile.Checksum, len(sources))
	for i, source := range sources {
		placeholders[i] = portfile.Checksum{Name: source.Name}
	}
	_, _, err := portfile.ReplaceChecksums(contents, info.Options["checksums"], placeholders...)
	return err
}
