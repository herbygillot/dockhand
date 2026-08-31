package port

import (
	"io"
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
//
// The copy keeps the port's <category>/<port> identity: the shadow of
// devel/foo lives at <tmp>/devel/foo, not at <tmp> itself. Evaluation
// never cares, but anything that stages a portdir by its layout — the
// verifier's overlay, whose indexer walks categories — reads the
// category from the path, and a shadow that discarded it could not be
// staged as the port it is a shadow of.
func (h Handle) Shadow(portfile []byte) (Handle, func(), error) {
	root, remove, err := h.TempDir.MakeDir("shadow")
	if err != nil {
		return Handle{}, nil, err
	}
	clean := filepath.Clean(h.Target.Portdir)
	dir := filepath.Join(root, filepath.Base(filepath.Dir(clean)), filepath.Base(clean))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		remove()
		return Handle{}, nil, err
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
