package tart

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// keptDirectory is where dockhand marks the check clones it keeps for one
// Tart home: ~/.dockhand/tart-kept, keyed by the canonical Tart home, as its
// locks are.
func keptDirectory(home string) (string, error) {
	return homeDirectory(home, "tart-kept")
}

// KeepClone marks a check's clone as kept, now, so cleanup knows it from
// one a process that died left.
func KeepClone(home, name string) error {
	directory, err := keptDirectory(home)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, name), nil, 0o600)
}

// KeptAt is when a clone was kept, and false for one that wasn't.
func KeptAt(home, name string) (time.Time, bool) {
	directory, err := keptDirectory(home)
	if err != nil {
		return time.Time{}, false
	}
	info, err := os.Stat(filepath.Join(directory, name))
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// ForgetClone takes a clone's mark away, as it's deleted.
func ForgetClone(home, name string) error {
	directory, err := keptDirectory(home)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(directory, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
