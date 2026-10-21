package tart

import (
	"errors"
	"os"
	"path/filepath"
)

// Resolve applies runtime defaults without creating directories or invoking
// Tart. Dockhand's Tart home is its own, ~/.dockhand/tart, or
// DOCKHAND_TART_HOME, as compose places it, and never the person's: a VM
// of theirs, such as a running one with an ASIF disk that keeps Tart from
// listing (openai/tart#1344), stays out of dockhand's view (decision 42).
func (c Client) Resolve() (Client, error) {
	if c.Executable == "" {
		c.Executable = "tart"
	}
	if c.Home == "" {
		return c, errors.New("tart: no Tart home was given for dockhand's images")
	}
	var err error
	c.Home, err = CanonicalDirectory(c.Home)
	return c, err
}

// CanonicalDirectory resolves existing ancestors so coordination uses the same
// path through aliases, including when the final directory has not been created.
func CanonicalDirectory(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var missing []string
	for {
		info, err := os.Lstat(path)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				info, err = os.Stat(resolved)
				if err != nil {
					return "", err
				}
			}
			if !info.IsDir() {
				return "", &os.PathError{Op: "resolve directory", Path: path, Err: errors.New("not a directory")}
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		missing = append(missing, filepath.Base(path))
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		path = parent
	}
}
