package tart

import (
	"context"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/v2/internal/filelock"
)

// AcquireImageRead keeps an image available as an immutable clone source.
func AcquireImageRead(ctx context.Context, home, image string) (*os.File, error) {
	return filelock.Acquire(ctx, imageLockPath(home, "image", image), filelock.Shared)
}

// AcquireImageWrite excludes readers while replacing an image.
func AcquireImageWrite(ctx context.Context, home, image string) (*os.File, error) {
	return filelock.Acquire(ctx, imageLockPath(home, "image", image), filelock.Exclusive)
}

// AcquireProvisioning serializes setup recipes targeting the same image.
func AcquireProvisioning(ctx context.Context, home, image string) (*os.File, error) {
	return filelock.Acquire(ctx, imageLockPath(home, "setup", image), filelock.Exclusive)
}

func imageLockPath(home, kind, image string) string {
	return filepath.Join(home, "dockhand", "locks", kind+"-"+digest([]byte(image))+".lock")
}
