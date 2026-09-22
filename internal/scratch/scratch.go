// Package scratch is the process's transient directory: one run root under
// the system temporary directory that every short-lived directory dockhand
// makes is created inside, workspaces, their overlays, materialized sources,
// helper sandboxes, and edit buffers among them. The root is removed when
// the process ends normally, and it is held under an advisory lock while
// the process lives, so a root whose lock can be taken belongs to a process
// that died without cleaning up and is swept by the next process that opens
// a root, and by gc. Nothing meant to outlive a command lives here: the
// state database, verification artifacts and logs, the Tart pool, and the
// index cache have their own configured homes.
package scratch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// prefix names a run root; a hidden name marks one not yet locked.
	prefix   = "dockhand-run-"
	lockName = ".lock"
	// abandoned is how old a root without a lock may be before the sweep
	// treats it as left behind between creation and locking.
	abandoned = 10 * time.Minute
)

var (
	mu   sync.Mutex
	root string
	lock *os.File
)

// Dir creates a new directory under the process's run root, named by the
// pattern as os.MkdirTemp names one. The caller removes it when done; the
// root's removal at exit, or a later sweep, catches what it did not.
func Dir(pattern string) (string, error) {
	r, err := Root()
	if err != nil {
		return "", err
	}
	return os.MkdirTemp(r, pattern)
}

// Root returns the process's run root, creating and locking it on first
// use. A creation also sweeps the roots of dead processes, on a
// best-effort basis. A root that was removed from under the process, as a
// test's temporary directory is removed when the test ends, is replaced by
// a new one under the temporary directory of the moment.
func Root() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if root != "" {
		if _, err := os.Stat(root); err == nil {
			return root, nil
		}
		_ = lock.Close()
		root, lock = "", nil
	}
	created, held, err := create(os.TempDir())
	if err != nil {
		return "", err
	}
	root, lock = created, held
	_, _ = sweep(os.TempDir(), created, time.Time{}, false)
	return root, nil
}

// create makes a root under parent: a hidden directory first, locked, then
// renamed into view, so no sweeper can find it before its lock is held.
func create(parent string) (string, *os.File, error) {
	hidden, err := os.MkdirTemp(parent, "."+prefix)
	if err != nil {
		return "", nil, fmt.Errorf("scratch: %w", err)
	}
	held, err := os.OpenFile(filepath.Join(hidden, lockName), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("scratch: %w", err), os.RemoveAll(hidden))
	}
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return "", nil, errors.Join(fmt.Errorf("scratch: lock run root: %w", err), held.Close(), os.RemoveAll(hidden))
	}
	visible := filepath.Join(parent, strings.TrimPrefix(filepath.Base(hidden), "."))
	if err := os.Rename(hidden, visible); err != nil {
		return "", nil, errors.Join(fmt.Errorf("scratch: %w", err), held.Close(), os.RemoveAll(hidden))
	}
	visible, err = filepath.EvalSymlinks(visible)
	if err != nil {
		return "", nil, errors.Join(fmt.Errorf("scratch: %w", err), held.Close(), os.RemoveAll(visible))
	}
	return visible, held, nil
}

// Close removes the process's run root and releases its lock. A later Dir
// makes a new root.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if root == "" {
		return nil
	}
	directory, held := root, lock
	root, lock = "", nil
	err := os.RemoveAll(directory)
	return errors.Join(err, held.Close())
}

// Stale lists the run roots under the system temporary directory whose
// process is gone: those whose lock can be taken, and hidden ones left
// unlocked for longer than a new root takes to lock. The process's own
// root is never listed. With a non-zero legacyBefore, it also lists the
// directories an earlier dockhand build made outside a run root, every
// dockhand-* name, last modified before that time; those carry no lock,
// so age is the only evidence a process is not still using one.
func Stale(legacyBefore time.Time) ([]string, error) {
	mu.Lock()
	own := root
	mu.Unlock()
	return sweep(os.TempDir(), own, legacyBefore, true)
}

// Sweep removes what Stale lists and returns the directories it removed. A
// root another sweeper is removing at the same time is left to it.
func Sweep(legacyBefore time.Time) ([]string, error) {
	mu.Lock()
	own := root
	mu.Unlock()
	return sweep(os.TempDir(), own, legacyBefore, false)
}

// legacy is the name every transient directory shared before run roots.
const legacy = "dockhand-"

func sweep(parent, own string, legacyBefore time.Time, dryRun bool) ([]string, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, fmt.Errorf("scratch: %w", err)
	}
	var found []string
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(parent, name)
		var stale bool
		release := func() {}
		switch {
		case strings.HasPrefix(name, "."+prefix):
			// A hidden root is being made: its lock file exists before it
			// is locked, so trying the lock would steal a live root. Age
			// alone says it was abandoned before it could be published.
			stale = abandonedFor(entry, abandoned)
		case strings.HasPrefix(name, prefix):
			if resolved, err := filepath.EvalSymlinks(directory); err == nil && resolved == own {
				continue
			}
			var err error
			stale, release, err = dead(directory, entry)
			if err != nil {
				errs = append(errs, err)
				continue
			}
		case strings.HasPrefix(name, legacy) && !legacyBefore.IsZero():
			info, err := entry.Info()
			stale = err == nil && info.ModTime().Before(legacyBefore)
		}
		if !stale {
			continue
		}
		if dryRun {
			release()
			found = append(found, directory)
			continue
		}
		if err := os.RemoveAll(directory); err != nil {
			errs = append(errs, err)
		} else {
			found = append(found, directory)
		}
		release()
	}
	return found, errors.Join(errs...)
}

// abandonedFor reports whether a directory was last modified longer ago
// than the given age.
func abandonedFor(entry os.DirEntry, age time.Duration) bool {
	info, err := entry.Info()
	return err == nil && time.Since(info.ModTime()) > age
}

// dead reports whether a published run root's process is gone. Every
// published root was locked before it was published, so it takes the
// root's lock when it can, and the release returned frees it after the
// root is removed, so a concurrent sweeper finds it held or gone.
func dead(directory string, entry os.DirEntry) (bool, func(), error) {
	release := func() {}
	held, err := os.OpenFile(filepath.Join(directory, lockName), os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		// A published root without its lock file is damaged; age alone
		// says nothing is using it.
		return abandonedFor(entry, abandoned), release, nil
	}
	if err != nil {
		return false, release, fmt.Errorf("scratch: %w", err)
	}
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return false, release, held.Close()
	}
	return true, func() { _ = held.Close() }, nil
}
