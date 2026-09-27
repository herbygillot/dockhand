package tart

import (
	"context"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/model"
)

// AcquireImageRead keeps an image available as an immutable clone source.
func AcquireImageRead(ctx context.Context, home, image string) (*os.File, error) {
	return acquire(ctx, home, "image", image, filelock.Shared)
}

// AcquireImageWrite excludes readers while replacing an image.
func AcquireImageWrite(ctx context.Context, home, image string) (*os.File, error) {
	return acquire(ctx, home, "image", image, filelock.Exclusive)
}

// AcquireProvisioning serializes setup recipes targeting the same image.
func AcquireProvisioning(ctx context.Context, home, image string) (*os.File, error) {
	return acquire(ctx, home, "setup", image, filelock.Exclusive)
}

func acquire(ctx context.Context, home, kind, image string, mode filelock.Mode) (*os.File, error) {
	directory, err := LockDirectory(home)
	if err != nil {
		return nil, err
	}
	return filelock.Acquire(ctx, filepath.Join(directory, kind+"-"+model.Digest([]byte(image))+".lock"), mode)
}

// LockDirectory is where dockhand's locks on one Tart home's images live:
// in dockhand's own directory, ~/.dockhand/tart-locks, keyed by the
// canonical Tart home, since nothing inside a Tart home is dockhand's to
// write. It is per user rather than per database, so every dockhand
// process sharing the Tart home shares the locks.
func LockDirectory(home string) (string, error) {
	return homeDirectory(home, "tart-locks")
}

// homeDirectory is a directory of dockhand's own, ~/.dockhand/<kind>,
// for one Tart home, keyed by its canonical path.
func homeDirectory(home, kind string) (string, error) {
	home, err := CanonicalDirectory(home)
	if err != nil {
		return "", err
	}
	user, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(user, ".dockhand", kind, model.Digest([]byte(home))), nil
}
