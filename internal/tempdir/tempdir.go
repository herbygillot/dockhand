// Package tempdir owns a run's temporary working space: the shadowed
// portdirs a planner evaluates, the distfiles it fetches, the files it
// stages for a generator. Every one of those used to be created and
// removed on its own, so a run that died between the two left
// directories no one could attribute to it.
//
// A Root gathers them under one directory named for the process, so
// what a run left behind is one identifiable tree rather than several
// anonymous ones. Removing the Root removes all of it, which is the
// last-resort cleanup; the per-use removers MakeDir returns are still
// the working mechanism, because a sweep that only freed its space at
// exit would hold every distfile it ever fetched.
//
// The zero Root is usable and means the system temporary directory,
// which is what every one of these sites did before. A package that
// takes a Root therefore needs no special case for callers — tests
// included — that have no run to belong to.
package tempdir

import (
	"fmt"
	"os"
)

// Root is a run's temporary directory. The zero Root issues its
// directories straight from the system temporary directory and owns
// nothing, so Remove on it does nothing.
//
// Not safe for concurrent use by multiple goroutines.
type Root struct {
	dir string
}

// New creates a run-scoped root under the system temporary directory,
// named for the process that owns it.
//
// The name carries the pid so a leftover tree can be attributed, but the
// pid alone would not be unique: pids are recycled, and a root left by a
// killed run would then be adopted by a later run that drew the same
// number. The random suffix settles that, and the pid stays readable.
func New() (Root, error) {
	dir, err := os.MkdirTemp("", fmt.Sprintf("dockhand-%d-", os.Getpid()))
	if err != nil {
		return Root{}, fmt.Errorf("tempdir: %w", err)
	}
	return Root{dir: dir}, nil
}

// Path is the root's directory, empty for the zero Root.
func (r Root) Path() string { return r.dir }

// MakeDir issues a directory for one purpose and a function that removes
// it. The purpose names the directory, so a tree left behind after a
// crash says what each part of it was for.
//
// The remover is returned rather than left to the caller to compose,
// following Handle.Shadow: the only thing safe to remove is what MakeDir
// created, and a caller assembling that path itself is a caller that can
// assemble the wrong one.
func (r Root) MakeDir(purpose string) (string, func(), error) {
	dir, err := os.MkdirTemp(r.dir, purpose+"-")
	if err != nil {
		return "", nil, fmt.Errorf("tempdir: %s: %w", purpose, err)
	}
	return dir, func() { os.RemoveAll(dir) }, nil //nolint:errcheck // best-effort cleanup
}

// Remove deletes the root and everything still under it. The zero Root
// owns no directory and removes nothing.
func (r Root) Remove() error {
	if r.dir == "" {
		return nil
	}
	return os.RemoveAll(r.dir)
}
