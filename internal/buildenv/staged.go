package buildenv

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// StagedTree is the file a provider stages a check's revision into, in
// its attempt's directory, for the build environment to unpack: the
// ports tree, the guest program, and its input.
const StagedTree = "input.tar"

// RemoveStagedTrees removes every staged tree under a check's log
// directory, its attempts' among them, and says how many it removed and
// their bytes: a process stopped mid-build never removed its own, and the
// check resumed in the next attempt's directory, so the stopped attempt's
// 173 MB stayed beside its log (Prime-time's v0.7.0 acceptance). A
// directory that isn't there removes nothing.
func RemoveStagedTrees(directory string) (int, int64, error) {
	var count int
	var bytes int64
	var errs []error
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() || entry.Name() != StagedTree {
			return nil
		}
		info, statErr := entry.Info()
		if removeErr := os.Remove(path); removeErr != nil {
			errs = append(errs, removeErr)
			return nil
		}
		count++
		if statErr == nil {
			bytes += info.Size()
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	return count, bytes, errors.Join(append(errs, err)...)
}
