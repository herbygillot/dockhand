// Package tree knows the shape of a ports tree on disk: where portdirs
// are, how to enumerate them, and how to find a port by name —
// including ResolveTarget, which turns any reference a MacPorts user
// writes (a name, a path, a category/dir) into a portdir, consulting
// the tree's own PortIndex for names. It reads directory structure and
// the index only — what a port means is the evaluator's business.
package tree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
)

// ErrNotPortsTree reports that a path does not look like a ports tree.
var ErrNotPortsTree = errors.New("tree: not a ports tree")

// ErrPortNotFound reports that no portdir matches the requested name.
var ErrPortNotFound = errors.New("tree: port not found")

// Tree is a ports tree rooted at a directory, validated on open by the
// presence of the PortGroup resources directory — the one structural
// feature every checkout shares. Resolve caches the tree's PortIndex on
// first use; a Tree is not safe for concurrent use.
type Tree struct {
	root string

	idx       *portindex.Index
	idxErr    error
	idxLoaded bool
}

// Open validates and returns the tree rooted at path.
func Open(path string) (*Tree, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(filepath.Join(abs, macports.PortGroupDir)); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%w: %s (no %s)", ErrNotPortsTree, abs, macports.PortGroupDir)
	}
	return &Tree{root: abs}, nil
}

// Root returns the tree's root directory.
func (t *Tree) Root() string { return t.root }

// categories lists category directories: non-hidden, non-underscore
// directories at the root.
func (t *Tree) categories() ([]string, error) {
	entries, err := os.ReadDir(t.root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// Portdirs enumerates every portdir in the tree: directories two levels
// down that contain a Portfile, in lexical order.
func (t *Tree) Portdirs() ([]string, error) {
	cats, err := t.categories()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, cat := range cats {
		dirs, err := t.CategoryPortdirs(cat)
		if err != nil {
			return nil, err
		}
		out = append(out, dirs...)
	}
	return out, nil
}

// CategoryPortdirs enumerates the portdirs of one category.
func (t *Tree) CategoryPortdirs(category string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(t.root, category))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		dir := filepath.Join(t.root, category, e.Name())
		if _, err := os.Stat(filepath.Join(dir, macports.PortfileName)); err == nil {
			out = append(out, dir)
		}
	}
	return out, nil
}

// HasCategory reports whether the tree has a category of the given name.
func (t *Tree) HasCategory(name string) bool {
	fi, err := os.Stat(filepath.Join(t.root, name))
	return err == nil && fi.IsDir() && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_")
}

// Lookup finds the portdir whose directory name matches the port name.
// Directory name and port name coincide for essentially the whole tree;
// the authoritative name is still whatever evaluation reports.
func (t *Tree) Lookup(name string) (string, error) {
	cats, err := t.categories()
	if err != nil {
		return "", err
	}
	for _, cat := range cats {
		dir := filepath.Join(t.root, cat, name)
		if _, err := os.Stat(filepath.Join(dir, macports.PortfileName)); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrPortNotFound, name)
}
