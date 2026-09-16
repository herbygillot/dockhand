package portedit

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
)

func (s *Service) prepareChecksums(ctx context.Context, request Request, input *sourceInput) (Result, error) {
	result := Result{Base: request.Source, Target: input.target}
	sources, err := downloadSources(input.info, filepath.Join(input.files.Root, filepath.Dir(input.target.Portfile)))
	if err != nil {
		return result, err
	}
	contents, checksums, downloads, err := s.refreshArchives(ctx, input.data, input.info, sources)
	if err != nil {
		return result, err
	}
	result.Downloads = downloads
	edit, after, root, err := s.evaluateEdit(ctx, request, input, contents)
	if err != nil {
		return result, err
	}
	fidelity := checksumFidelity(input.before, after, input.target.Name, input.files.Root, root, checksums)
	result.Fidelity = []Fidelity{fidelity}
	if len(fidelity.UnexpectedChanges) > 0 {
		return result, fmt.Errorf("%w: %v", ErrFidelity, fidelity.UnexpectedChanges)
	}
	if bytes.Equal(contents, input.data) {
		return result, nil
	}
	result.Files = []portfile.Edit{edit}
	result.Commits = []CommitIntent{{Subject: input.target.Name + ": refresh checksums", Body: request.Reason, Paths: []string{input.target.Portfile}}}
	return result, nil
}

func (s *Service) refreshArchives(ctx context.Context, contents []byte, info macports.PortInfo, sources []archiveSource) ([]byte, string, []Download, error) {
	var err error
	if err := checkChecksumSources(contents, info, sources); err != nil {
		return nil, "", nil, err
	}
	downloads := make([]Download, 0, len(sources))
	for _, source := range sources {
		var output io.Writer
		var file *os.File
		if s.archiveDirectory != "" {
			file, err = os.CreateTemp(s.archiveDirectory, "source-*")
			if err != nil {
				return nil, "", nil, err
			}
			output = file
		}
		download, err := s.downloadArchive(ctx, info, source, output)
		if file != nil {
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
			download.path = file.Name()
		}
		if err != nil {
			return nil, "", nil, err
		}
		downloads = append(downloads, download)
	}
	contents, checksums, err := portfile.ReplaceChecksums(contents, info.Options["checksums"], checksumValues(downloads)...)
	if err != nil {
		return nil, "", nil, err
	}
	return contents, checksums, downloads, nil
}

func checkChecksumSources(contents []byte, info macports.PortInfo, sources []archiveSource) error {
	placeholders := make([]portfile.Checksum, len(sources))
	for i, source := range sources {
		placeholders[i] = portfile.Checksum{Name: source.Name}
	}
	_, _, err := portfile.ReplaceChecksums(contents, info.Options["checksums"], placeholders...)
	return err
}
