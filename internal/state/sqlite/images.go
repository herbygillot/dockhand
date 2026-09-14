package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/state"
)

func (s *Store) ImageDigest(ctx context.Context, provider, path string) (state.ImageDigest, error) {
	value := state.ImageDigest{Provider: provider, Path: path}
	err := s.transaction(ctx, false, "", func(ctx context.Context, t *transaction) error {
		return storageError(t.conn.QueryRowContext(ctx, "SELECT stamp,digest FROM image_digests WHERE provider=? AND path=?", provider, path).Scan(&value.Stamp, &value.Digest))
	})
	return value, err
}

func (s *Store) ImageCapabilities(ctx context.Context, provider, environmentDigest string) (state.ImageCapabilities, error) {
	value := state.ImageCapabilities{Provider: provider, EnvironmentDigest: environmentDigest}
	var raw string
	var observed int64
	err := s.transaction(ctx, false, "", func(ctx context.Context, t *transaction) error {
		return storageError(t.conn.QueryRowContext(ctx, `SELECT capability_digest,capabilities,problem,observed_at
            FROM image_capabilities WHERE provider=? AND environment_digest=?`, provider, environmentDigest).Scan(&value.CapabilityDigest, &raw, &value.Problem, &observed))
	})
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal([]byte(raw), &value.Capabilities); err != nil {
		return value, storageError(err)
	}
	value.ObservedAt = time.UnixMilli(observed).UTC()
	return value, nil
}

func (s *Store) PutImageCapabilities(ctx context.Context, value state.ImageCapabilities) error {
	if value.Provider == "" || value.EnvironmentDigest == "" || value.CapabilityDigest == "" || value.ObservedAt.IsZero() {
		return state.ErrInvalid
	}
	raw, err := json.Marshal(value.Capabilities)
	if err != nil {
		return state.ErrInvalid
	}
	return s.transaction(ctx, true, "", func(ctx context.Context, t *transaction) error {
		return t.exec(ctx, `INSERT INTO image_capabilities(provider,environment_digest,capability_digest,capabilities,problem,observed_at)
            VALUES(?,?,?,?,?,?) ON CONFLICT(provider,environment_digest) DO UPDATE SET
            capability_digest=excluded.capability_digest,capabilities=excluded.capabilities,problem=excluded.problem,observed_at=excluded.observed_at`,
			value.Provider, value.EnvironmentDigest, value.CapabilityDigest, string(raw), value.Problem, value.ObservedAt.UTC().UnixMilli())
	})
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
