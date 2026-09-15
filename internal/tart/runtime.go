package tart

import (
	"errors"
	"os"
	"path/filepath"
)

// Resolve applies runtime defaults without creating directories or invoking Tart.
func (c Client) Resolve() (Client, error) {
	if c.Executable == "" {
		c.Executable = "tart"
	}
	if c.Home == "" {
		c.Home = os.Getenv("TART_HOME")
	}
	if c.Home == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return c, err
		}
		c.Home = filepath.Join(home, ".tart")
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
