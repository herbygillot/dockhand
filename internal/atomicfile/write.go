package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Write replaces a file through a synced sibling and syncs its parent directory.
// Parent directories are created with mode 0700 when absent.
func Write(path string, data []byte, mode fs.FileMode) error {
	return Create(path, mode, func(file *os.File) error {
		_, err := file.Write(data)
		return err
	})
}

// Create replaces a file with contents a callback writes to a synced sibling,
// then renames it into place and syncs the parent directory. A failing callback
// leaves the destination untouched. Parent directories are created with mode
// 0700 when absent.
func Create(path string, mode fs.FileMode, write func(*os.File) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".dockhand-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		err = write(file)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

// ReplaceDirectory builds a directory in a temporary sibling and moves it into
// place. A previous directory is retired only after the replacement exists, so
// a crash leaves either the old or the new directory, never neither. A failing
// build leaves the destination untouched.
func ReplaceDirectory(destination string, build func(temp string) error) (err error) {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(parent, ".new-")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(temp))
		}
	}()
	if err = build(temp); err != nil {
		return err
	}
	retired := ""
	if _, statErr := os.Lstat(destination); statErr == nil {
		retired, err = os.MkdirTemp(parent, ".old-")
		if err != nil {
			return err
		}
		if err = os.Remove(retired); err != nil {
			return err
		}
		if err = os.Rename(destination, retired); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err = os.Rename(temp, destination); err != nil {
		if retired != "" {
			err = errors.Join(err, os.Rename(retired, destination))
		}
		return err
	}
	if retired != "" {
		if err = os.RemoveAll(retired); err != nil {
			return err
		}
	}
	return syncDirectory(parent)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
