package tree

import (
	"io"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports"
)

// Shadow materializes a copy of a portdir with its Portfile replaced by
// the given bytes, in a fresh temporary directory — the surface a
// planner evaluates to learn, exactly, what its edits would do. The
// caller removes the returned directory. Regular contents (files/,
// patches) are copied; symlinks — a work link from a local build, say —
// are not part of the port and are skipped.
func Shadow(portdir string, portfile []byte) (string, error) {
	dir, err := os.MkdirTemp("", "dockhand-shadow-*")
	if err != nil {
		return "", err
	}
	if err := copyTree(dir, portdir); err != nil {
		os.RemoveAll(dir) //nolint:errcheck // best-effort on the error path
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, macports.PortfileName), portfile, 0o644); err != nil {
		os.RemoveAll(dir) //nolint:errcheck
		return "", err
	}
	return dir, nil
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
