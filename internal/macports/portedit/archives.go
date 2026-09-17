package portedit

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
)

// archiveStore fetches source archives. With a directory it keeps each
// archive's bytes there and records the path, so dependency generators can
// extract them; without one it only hashes the stream.
type archiveStore struct {
	service   *Service
	directory string
}

func (s *Service) archives(directory string) *archiveStore {
	return &archiveStore{service: s, directory: directory}
}

// fetch downloads one archive, keeping its bytes when the store has a directory.
func (a *archiveStore) fetch(ctx context.Context, info macports.PortInfo, source archiveSource) (Download, error) {
	if a.directory == "" {
		return a.service.downloadArchive(ctx, info, source, nil)
	}
	file, err := os.CreateTemp(a.directory, "source-*")
	if err != nil {
		return Download{}, err
	}
	download, err := a.service.downloadArchive(ctx, info, source, file)
	err = errors.Join(err, file.Close())
	if err != nil {
		return Download{}, errors.Join(err, os.Remove(file.Name()))
	}
	download.path = file.Name()
	return download, nil
}

// fetchFirst tries each location in order and keeps the first archive that
// downloads; a canceled context stops the sequence.
func (a *archiveStore) fetchFirst(ctx context.Context, info macports.PortInfo, name string, locations []string) (Download, error) {
	var err error
	for _, address := range locations {
		var download Download
		download, err = a.fetch(ctx, info, archiveSource{Name: name, URL: address})
		if err == nil {
			return download, nil
		}
		if ctx.Err() != nil {
			return Download{}, ctx.Err()
		}
	}
	return Download{}, err
}

// refresh downloads every declared archive and writes their checksums into contents.
func (a *archiveStore) refresh(ctx context.Context, contents []byte, info macports.PortInfo, sources []archiveSource) ([]byte, string, []Download, error) {
	if err := checkChecksumSources(contents, info, sources); err != nil {
		return nil, "", nil, err
	}
	downloads := make([]Download, 0, len(sources))
	for _, source := range sources {
		download, err := a.fetch(ctx, info, source)
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

// commitEdit records one evaluated edit as the result's single commit unless
// fidelity found unexpected changes. Files and fidelity are recorded either
// way so callers can report what was attempted.
func (r *Result) commitEdit(input *sourceInput, request Request, edit portfile.Edit, report Fidelity, subject string) error {
	r.Files = []portfile.Edit{edit}
	r.Fidelity = append(r.Fidelity, report)
	if len(report.UnexpectedChanges) > 0 {
		return fmt.Errorf("%w: %v", ErrFidelity, report.UnexpectedChanges)
	}
	name := input.target.Name
	if request.CommitName != "" {
		name = request.CommitName
	}
	r.Commits = []CommitIntent{{Subject: name + ": " + subject, Body: request.Reason, Paths: []string{input.target.Portfile}}}
	return nil
}
