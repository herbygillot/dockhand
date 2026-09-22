package archives

import (
	"context"
	"errors"
	"os"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
)

// Store fetches source archives through its client. With a directory it
// keeps each archive's bytes there and records the path, so dependency
// generators and the patch check can extract them; without one it only
// hashes the stream.
type Store struct {
	client    Client
	directory string
}

// Store binds the client to a directory for kept archives, or to none.
func (c Client) Store(directory string) *Store {
	return &Store{client: c, directory: directory}
}

// Fetch downloads one archive, keeping its bytes when the store has a directory.
func (s *Store) Fetch(ctx context.Context, info macports.PortInfo, source Source) (Download, error) {
	if s.directory == "" {
		return s.client.download(ctx, info, source, nil)
	}
	file, err := os.CreateTemp(s.directory, "source-*")
	if err != nil {
		return Download{}, err
	}
	download, err := s.client.download(ctx, info, source, file)
	err = errors.Join(err, file.Close())
	if err != nil {
		return Download{}, errors.Join(err, os.Remove(file.Name()))
	}
	download.Path = file.Name()
	return download, nil
}

// FetchFirst tries each location in order and keeps the first archive that
// downloads; a canceled context stops the sequence.
func (s *Store) FetchFirst(ctx context.Context, info macports.PortInfo, name string, locations []string) (Download, error) {
	var err error
	for _, address := range locations {
		var download Download
		download, err = s.Fetch(ctx, info, Source{Name: name, URL: address})
		if err == nil {
			return download, nil
		}
		if ctx.Err() != nil {
			return Download{}, ctx.Err()
		}
	}
	return Download{}, err
}

// Refresh downloads every declared archive and writes their checksums into
// contents, keeping legacy groups as written when asked.
func (s *Store) Refresh(ctx context.Context, contents []byte, info macports.PortInfo, sources []Source, keepLegacy bool) ([]byte, string, []Download, error) {
	if err := CheckChecksumSources(contents, info, sources); err != nil {
		return nil, "", nil, err
	}
	downloads := make([]Download, 0, len(sources))
	for _, source := range sources {
		download, err := s.Fetch(ctx, info, source)
		if err != nil {
			return nil, "", nil, err
		}
		downloads = append(downloads, download)
	}
	contents, checksums, err := portfile.ReplaceChecksumsKeeping(contents, info.Options["checksums"], keepLegacy, ChecksumValues(downloads)...)
	if err != nil {
		return nil, "", nil, err
	}
	return contents, checksums, downloads, nil
}

// CheckChecksumSources checks that the checksum declaration can be rewritten
// for every source before any download is spent.
func CheckChecksumSources(contents []byte, info macports.PortInfo, sources []Source) error {
	placeholders := make([]portfile.Checksum, len(sources))
	for i, source := range sources {
		placeholders[i] = portfile.Checksum{Name: source.Name}
	}
	_, _, err := portfile.ReplaceChecksums(contents, info.Options["checksums"], placeholders...)
	return err
}

// ChecksumValues is the checksum declaration each download makes.
func ChecksumValues(downloads []Download) []portfile.Checksum {
	values := make([]portfile.Checksum, len(downloads))
	for i, d := range downloads {
		values[i] = portfile.Checksum{Name: d.Name, SHA256: d.SHA256, RMD160: d.RMD160, MD5: d.MD5, SHA1: d.SHA1, Size: d.Size}
	}
	return values
}
