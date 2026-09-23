package archives

import (
	"context"
	"errors"
	"os"

	"github.com/herbygillot/dockhand/internal/macports"
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
