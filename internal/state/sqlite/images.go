package sqlite

import (
	"context"
	"path/filepath"

	"github.com/herbygillot/dockhand/v2/internal/state"
)

func (s *Store) ImageDigest(ctx context.Context, provider, path string) (state.ImageDigest, error) {
	value := state.ImageDigest{Provider: provider, Path: path}
	err := s.transaction(ctx, false, "", func(ctx context.Context, t *transaction) error {
		return storageError(t.conn.QueryRowContext(ctx, "SELECT stamp,digest FROM image_digests WHERE provider=? AND path=?", provider, path).Scan(&value.Stamp, &value.Digest))
	})
	return value, err
}

func (s *Store) PutImageDigest(ctx context.Context, value state.ImageDigest) error {
	if value.Provider == "" || !filepath.IsAbs(value.Path) || value.Stamp == "" || value.Digest == "" {
		return state.ErrInvalid
	}
	return s.transaction(ctx, true, "", func(ctx context.Context, t *transaction) error {
		return t.exec(ctx, `INSERT INTO image_digests(provider,path,stamp,digest) VALUES(?,?,?,?)
            ON CONFLICT(provider,path) DO UPDATE SET stamp=excluded.stamp,digest=excluded.digest`, value.Provider, value.Path, value.Stamp, value.Digest)
	})
}
