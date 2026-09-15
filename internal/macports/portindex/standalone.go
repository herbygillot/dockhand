package portindex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// The caller holds the profile cache lock. The hint is only a seed: the complete
// tree diff determines invalidation, and standalone gaps never certify a change.
func standaloneIndex(ctx context.Context, repo *git.Repository, source record.Source, platform record.Platform, c Config, cacheRoot, root string) (string, error) {
	directory := filepath.Join(cacheRoot, "standalone")
	target := filepath.Join(directory, string(source.Tree))
	if validIndexEntry(target) {
		return target, nil
	}
	var seed string
	var changed []string
	hint := filepath.Join(directory, "latest")
	data, err := os.ReadFile(hint)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	previous := strings.TrimSpace(string(data))
	if repo != nil && git.ValidObjectID(previous) && validIndexEntry(filepath.Join(directory, previous)) {
		paths, diffErr := repo.ChangedPaths(ctx, previous, string(source.Tree))
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if diffErr == nil && !requiresFullIndex(paths) {
			seed = filepath.Join(directory, previous)
			changed = paths
		}
	}
	if err := buildPortIndex(ctx, c, platform, root, target, seed, changed, false); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(directory, ".latest-")
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	_, writeErr := temp.WriteString(string(source.Tree) + "\n")
	err = errors.Join(writeErr, temp.Close())
	if err != nil {
		return "", err
	}
	if err := os.Rename(temp.Name(), hint); err != nil {
		return "", err
	}
	return target, nil
}
