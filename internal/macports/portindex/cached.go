package portindex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/git"
)

// CachedTrees are the trees a completed generation is cached for under the
// cache root, in any indexing environment, for a reader that takes an
// index only where one is cached already. It reads directories alone:
// nothing is built, fetched, locked, or touched, and an absent cache has
// none.
func CachedTrees(directory string) (map[string]bool, error) {
	trees := map[string]bool{}
	profiles, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return trees, nil
	}
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if !profile.IsDir() || !cacheHash(profile.Name()) {
			continue
		}
		generations, err := os.ReadDir(filepath.Join(directory, profile.Name(), generationsDirectory))
		if err != nil {
			continue
		}
		for _, entry := range generations {
			if entry.IsDir() && git.ValidObjectID(entry.Name()) && validIndexEntry(filepath.Join(directory, profile.Name(), generationsDirectory, entry.Name())) {
				trees[entry.Name()] = true
			}
		}
	}
	return trees, nil
}

// ReadCached reads the cached generation of a tree, from the first
// indexing environment, by name, that has one its metadata certifies, as
// usable does. Which environment doesn't matter to a reader of what every
// platform's index says alike, as a port's maintainers. The read runs
// under the environment's shared lock, so a cleanup doesn't take the
// generation meanwhile; an environment a cleanup holds is passed over.
// Nothing is built, fetched, or touched, so reading doesn't keep a
// generation from its cleanup either. Found is false where no environment
// has the tree.
func ReadCached(ctx context.Context, directory, tree string, read func(*Index) error) (bool, error) {
	if !git.ValidObjectID(tree) {
		return false, nil
	}
	profiles, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	names := []string{}
	for _, profile := range profiles {
		if profile.IsDir() && cacheHash(profile.Name()) {
			names = append(names, profile.Name())
		}
	}
	slices.Sort(names)
	for _, name := range names {
		found, err := readCachedIn(ctx, filepath.Join(directory, name), tree, read)
		if found || err != nil {
			return found, err
		}
	}
	return false, nil
}

// readCachedIn is ReadCached in one environment's directory.
func readCachedIn(ctx context.Context, profile, tree string, read func(*Index) error) (bool, error) {
	entry := filepath.Join(profile, generationsDirectory, tree)
	if !validIndexEntry(entry) {
		return false, nil
	}
	guard, err := filelock.TryExisting(ctx, filepath.Join(profile, cacheLockName), filelock.Shared)
	if errors.Is(err, filelock.ErrBusy) || errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer guard.Close()
	if meta, ok := readGeneration(entry); !ok || meta.Tree != tree || !validIndexEntry(entry) {
		return false, nil
	}
	index, err := Open(entry)
	if err != nil {
		return false, err
	}
	return true, read(index)
}
