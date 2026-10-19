package distfetch

import (
	"context"
	"errors"
	"fmt"
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
// downloads; a canceled context stops the sequence. Where every one sent an
// HTML page, as a mirror does for a file it doesn't carry yet, that's said:
// libpng's and four other SourceForge updates failed naming the last of
// twenty mirrors, zenlayer, as if it alone were wrong, and minutes later the
// mirrors had the release (field testing at 6dd7745c).
func (s *Store) FetchFirst(ctx context.Context, info macports.PortInfo, name string, locations []string) (Download, error) {
	var err error
	pages := 0
	for _, address := range locations {
		var download Download
		download, err = s.Fetch(ctx, info, Source{Name: name, URL: address})
		if err == nil {
			return download, nil
		}
		if ctx.Err() != nil {
			return Download{}, ctx.Err()
		}
		if errors.Is(err, errHTMLPage) {
			pages++
		}
	}
	if pages > 1 && pages == len(locations) {
		return Download{}, &MirrorsBehind{Sites: pages, Err: err}
	}
	return Download{}, err
}

// MirrorsBehind is an archive every one of its sites answered with an HTML
// page, as mirrors do for a file they don't carry yet: a new release takes
// a while to reach them, so another try later may find it.
type MirrorsBehind struct {
	Sites int
	Err   error
}

func (m *MirrorsBehind) Error() string {
	return fmt.Sprintf("%v; every one of its %d sites sent an HTML page, as a mirror does for a file it doesn't carry yet, so a new release may not have reached them; try again later", m.Err, m.Sites)
}

func (m *MirrorsBehind) Unwrap() error { return m.Err }

// Transient says another try later may find the archive (fetch.Transient).
func (m *MirrorsBehind) Transient() bool { return true }
