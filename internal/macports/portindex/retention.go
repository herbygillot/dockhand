package portindex

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/git"
)

type CacheRemoval struct {
	Path      string
	Completed bool
}

// Collect removes old disposable indexes under the same profile lock as Stage.
// Busy profiles are skipped. Neither previews nor absent caches create paths.
func Collect(ctx context.Context, directory string, before time.Time, dry bool) ([]CacheRemoval, error) {
	var result []CacheRemoval
	root, err := os.OpenRoot(directory)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer root.Close()
	profiles, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return result, err
	}
	for _, profile := range profiles {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !profile.IsDir() || !cacheHash(profile.Name()) {
			continue
		}
		lock, err := filelock.TryExisting(ctx, filepath.Join(directory, profile.Name(), "index.lock"), filelock.Exclusive)
		if errors.Is(err, filelock.ErrBusy) || errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return result, err
		}
		entries, err := collectProfile(ctx, root, profile.Name(), directory, before, dry)
		lock.Close()
		result = append(result, entries...)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func collectProfile(ctx context.Context, root *os.Root, profile, directory string, before time.Time, dry bool) ([]CacheRemoval, error) {
	var result []CacheRemoval
	err := fs.WalkDir(root.FS(), profile, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || path == profile {
			return nil
		}
		parts := strings.Split(strings.TrimPrefix(path, profile+"/"), "/")
		candidate := cacheEntry(parts)
		if !candidate {
			if len(parts) == 1 && (parts[0] == "complete" || parts[0] == "standalone" || parts[0] == "candidates") || len(parts) == 2 && parts[0] == "candidates" && git.ValidObjectID(parts[1]) {
				return nil
			}
			return fs.SkipDir
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(before) {
			return fs.SkipDir
		}
		item := CacheRemoval{Path: filepath.Join(directory, filepath.FromSlash(path))}
		result = append(result, item)
		if !dry {
			if err := root.RemoveAll(path); err != nil {
				return err
			}
			result[len(result)-1].Completed = true
		}
		return fs.SkipDir
	})
	return result, err
}

func cacheHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func cacheEntry(parts []string) bool {
	switch len(parts) {
	case 1:
		return git.ValidObjectID(parts[0])
	case 2:
		return (parts[0] == "complete" || parts[0] == "standalone") && git.ValidObjectID(parts[1])
	case 3:
		return parts[0] == "candidates" && git.ValidObjectID(parts[1]) && git.ValidObjectID(parts[2])
	}
	return false
}

func touchEntry(path string) error {
	now := time.Now()
	return os.Chtimes(path, now, now)
}
