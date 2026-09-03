package tree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
)

// Target is one resolved operation target: the portdir holding the
// Portfile, plus the subport name when the reference resolved through
// the index — the index names every subport, so a Target can be more
// precise than a directory. An empty Subport targets the Portfile's
// top-level port.
type Target struct {
	Portdir string // absolute portdir path
	Subport string // index-resolved port name; "" when resolved by location
}

// PathTarget reports whether ref names a portdir on disk — a directory
// holding a Portfile — and resolves it to an absolute Target. A path
// needs no ports tree: it says where the port is without being looked
// up.
func PathTarget(ref string) (Target, bool) {
	if _, err := os.Stat(filepath.Join(ref, macports.PortfileName)); err != nil {
		return Target{}, false
	}
	abs, err := filepath.Abs(ref)
	if err != nil {
		return Target{}, false
	}
	return Target{Portdir: abs}, true
}

// index lazily opens the tree's own PortIndex, caching the result —
// hit or miss — for the Tree's lifetime.
func (t *Tree) index() (*portindex.Index, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lockedIndex()
}

// lockedIndex is index with the caller already holding the lock, so
// that a cache built from the index fills both under one acquisition.
func (t *Tree) lockedIndex() (*portindex.Index, error) {
	if !t.idxLoaded {
		t.idx, t.idxErr = portindex.Open(t.root)
		t.idxLoaded = true
	}
	return t.idx, t.idxErr
}

// indexLookup resolves a name against the tree's index under the
// tree's lock. An Index rebuilds its own offsets when a stale quick
// file misroutes a lookup, so a lookup writes as well as reads and two
// concurrent resolutions of the same tree would race over it.
func (t *Tree) indexLookup(ref string) (portindex.Entry, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	idx, err := t.lockedIndex()
	if err != nil {
		return portindex.Entry{}, err
	}
	return idx.Lookup(ref)
}

// Resolve resolves the tree-bound reference forms: "category/dir" under
// the root, an indexed port or subport name, or a portdir's directory
// name (a filesystem path never needs a Tree — see PathTarget).
// The index consulted is always this tree's own — never another
// source's — so name and edit target come from the same tree; a stale
// index entry falls through to the directory walk, and whatever is
// resolved here is still re-evaluated downstream.
func (t *Tree) Resolve(ref string) (Target, error) {
	if _, err := os.Stat(filepath.Join(t.root, ref, macports.PortfileName)); err == nil {
		return Target{Portdir: filepath.Join(t.root, ref)}, nil
	}

	entry, idxErr := t.indexLookup(ref)
	if idxErr == nil {
		dir := filepath.Join(t.root, filepath.FromSlash(entry.Portdir))
		if _, err := os.Stat(filepath.Join(dir, macports.PortfileName)); err == nil {
			return Target{Portdir: dir, Subport: entry.Name}, nil
		}
	}

	if dir, err := t.Lookup(ref); err == nil {
		return Target{Portdir: dir}, nil
	}
	if errors.Is(idxErr, portindex.ErrNoIndex) {
		return Target{}, fmt.Errorf("%q: %w (the tree has no PortIndex; run portindex to enable name lookup)", ref, ErrPortNotFound)
	}
	return Target{}, fmt.Errorf("%q: %w", ref, ErrPortNotFound)
}

// ErrNoPortdir reports a Target used before resolution gave it a
// portdir.
var ErrNoPortdir = errors.New("tree: target has no portdir")

// Portfile returns the full path of the Target's Portfile.
func (t Target) Portfile() (string, error) {
	if t.Portdir == "" {
		return "", ErrNoPortdir
	}
	return filepath.Join(t.Portdir, macports.PortfileName), nil
}
