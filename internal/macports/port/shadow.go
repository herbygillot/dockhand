package port

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports"
)

// Shadow materializes a copy of this handle's portdir with its Portfile
// replaced by the given bytes, and returns a handle on that copy: the
// surface a planner evaluates to learn, exactly, what an edit would do
// before anything real is written.
//
// The returned function removes the copy. It is handed back rather than
// left to the caller because the only thing safe to remove is what
// Shadow created: a caller reaching for Target.Portdir to clean up
// would, on any other handle, be deleting a real portdir out of the
// ports tree. Skipping the call is how a shadow is kept for inspection.
//
// Regular contents (files/, patches) are copied; symlinks — a work link
// from a local build, say — are not part of the port and are skipped.
func (h Handle) Shadow(portfile []byte) (Handle, func(), error) {
	dir, err := os.MkdirTemp("", "dockhand-shadow-*")
	if err != nil {
		return Handle{}, nil, err
	}
	remove := func() {
		if err := os.RemoveAll(dir); err != nil {
			slog.Warn("shadow left behind", "dir", dir, "err", err)
		}
	}
	if err := copyTree(dir, h.Target.Portdir); err != nil {
		remove()
		return Handle{}, nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, macports.PortfileName), portfile, 0o644); err != nil {
		remove()
		return Handle{}, nil, err
	}
	return h.At(dir), remove, nil
}

// copyTree copies a portdir's regular contents into dst.
func copyTree(dst, srcDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.Type()&os.ModeSymlink != 0:
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			return copyFile(target, path)
		default:
			return nil
		}
	})
}

func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck // read-path close
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close() //nolint:errcheck // best-effort on the error path
		return err
	}
	return out.Close()
}
