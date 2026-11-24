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

// Why a clone was kept: asked, by check --keep, or for its failure,
// unasked (keep_failed), which cleanup removes after seven days.
const (
	KeptAsked  = "asked"
	KeptFailed = "failed"
)

// KeepClone marks a check's clone of image as kept, now, and why, so
// cleanup knows it from one a process that died left, and dockhand shell
// reaches it by its image's host keys.
func KeepClone(home, name, image, reason string) error {
	directory, err := keptDirectory(home)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, name), []byte(image), 0o600); err != nil {
		return err
	}
	if reason == KeptFailed {
		return os.WriteFile(filepath.Join(directory, name+failedSuffix), nil, 0o600)
	}
	return nil
}

// failedSuffix marks a clone kept for its failure, beside its mark.
const failedSuffix = ".failed"

// KeptFor is why a kept clone was kept, KeptAsked or KeptFailed.
func KeptFor(home, name string) string {
	directory, err := keptDirectory(home)
	if err != nil {
		return KeptAsked
	}
	if _, err := os.Stat(filepath.Join(directory, name+failedSuffix)); err == nil {
		return KeptFailed
	}
	return KeptAsked
}

// KeptAt is when a clone was kept, and false for one that wasn't.
func KeptAt(home, name string) (time.Time, bool) {
	info, kept := keptMarker(home, name)
	if info == nil {
		return time.Time{}, kept
	}
	return info.ModTime(), kept
}

// keptMarker is the one reading of whether a clone is kept: its marker is
// there. A marker that is there but can't be read, or can't be stat'd for
// any reason but its absence, is kept all the same, so a stop never deletes
// a clone the person asked to keep (architecture's ruling of 2026-11-24).
// info is the marker's, where it could be read.
func keptMarker(home, name string) (os.FileInfo, bool) {
	directory, err := keptDirectory(home)
	if err != nil {
		return nil, false
	}
	info, err := os.Stat(filepath.Join(directory, name))
	if err != nil {
		return nil, !errors.Is(err, os.ErrNotExist)
	}
	return info, true
}

// KeptFrom is the image a kept clone was cloned from, and false for a
// clone that wasn't kept: kept as keptMarker reads it, its image empty
// where the marker can't be read.
func KeptFrom(home, name string) (string, bool) {
	if _, kept := keptMarker(home, name); !kept {
		return "", false
	}
	directory, err := keptDirectory(home)
	if err != nil {
		return "", true
	}
	image, _ := os.ReadFile(filepath.Join(directory, name))
	return string(image), true
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
	if err := os.Remove(filepath.Join(directory, name+failedSuffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
