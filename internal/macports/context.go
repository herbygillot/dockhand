package macports

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
)

// Projection is what a tree can materialize of itself on demand. A sparse
// workspace holds a port's directory and _resources and brings more when a
// consumer asks; a plain materialization holds everything and needs
// nothing. Carried by the Tree, it says what the root is rather than
// leaving a consumer to find out by the root's path.
type Projection interface {
	// EnsurePort materializes the target's port directory and _resources.
	EnsurePort(ctx context.Context, target record.Target) error
	// EnsureAll materializes the whole tree.
	EnsureAll(ctx context.Context) error
	// Whole reports whether the root holds the whole tree.
	Whole() bool
}

// whole is a complete materialization: every ensure is satisfied already.
type whole struct{}

func (whole) EnsurePort(context.Context, record.Target) error { return nil }
func (whole) EnsureAll(context.Context) error                 { return nil }
func (whole) Whole() bool                                     { return true }

type Tree struct {
	source     record.Source
	root       string
	base       string
	platform   record.Platform
	projection Projection
	projected  bool
}

// NewTree is the tree of a plain, complete materialization. Production
// trees come from a workspace; tests build one here.
func NewTree(source record.Source, root string, platform record.Platform) (Tree, error) {
	return NewTreeOver(source, root, root, platform, nil)
}

// NewTreeOver is the tree of a projection, which brings more of the tree
// when a consumer asks; a nil projection is a complete materialization.
func NewTreeOver(source record.Source, root, base string, platform record.Platform, projection Projection) (Tree, error) {
	if source.Tree == "" || !filepath.IsAbs(root) || !filepath.IsAbs(base) {
		return Tree{}, fmt.Errorf("macports: source tree and absolute snapshot roots are required")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Tree{}, err
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return Tree{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return Tree{}, err
	}
	if !info.IsDir() {
		return Tree{}, fmt.Errorf("macports: snapshot root is not a directory")
	}
	tree := Tree{source: source, root: root, base: base, platform: platform, projection: projection, projected: projection != nil}
	if projection == nil {
		tree.projection = whole{}
	}
	return tree, nil
}

func (t Tree) Source() record.Source { return t.source }

// Projection is what the tree can materialize on demand; a plain
// materialization's satisfies every ensure already.
func (t Tree) Projection() Projection { return t.projection }

// Projected reports whether a workspace projects this tree, which the
// repository validated when it opened, as opposed to a plain directory.
func (t Tree) Projected() bool           { return t.projected }
func (t Tree) Root() string              { return t.root }
func (t Tree) Platform() record.Platform { return t.platform }

// Base is the root of the projection this tree overlays, or the root
// itself; an interpreter session bound to the base serves the overlay.
func (t Tree) Base() string { return t.base }

type Context struct {
	Tree
	target record.Target
}

func (t Tree) Select(target record.Target) (Context, error) {
	if t.root == "" || !ValidName(target.Name) || (target.Subport != "" && !ValidName(target.Subport)) || !portfilePath(target.Portfile) {
		return Context{}, fmt.Errorf("macports: a snapshot and category/port/Portfile target are required")
	}
	if err := validateVariants(target.Variants); err != nil {
		return Context{}, err
	}
	filename := filepath.Join(t.root, filepath.FromSlash(target.Portfile))
	resolved, err := filepath.EvalSymlinks(filename)
	if err != nil {
		return Context{}, err
	}
	rel, err := filepath.Rel(t.root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return Context{}, fmt.Errorf("macports: Portfile leaves snapshot")
	}
	if resolved != filename {
		return Context{}, fmt.Errorf("macports: symlinked Portfile paths are unsupported")
	}
	info, err := os.Stat(filename)
	if err != nil {
		return Context{}, err
	}
	if !info.Mode().IsRegular() {
		return Context{}, fmt.Errorf("macports: Portfile is not a regular file")
	}
	target.Variants = maps.Clone(target.Variants)
	return Context{Tree: t, target: target}, nil
}

func (c Context) Target() record.Target {
	target := c.target
	target.Variants = maps.Clone(target.Variants)
	return target
}

func portfilePath(name string) bool {
	parts := strings.Split(name, "/")
	return fs.ValidPath(name) && len(parts) == 3 && parts[2] == "Portfile" && !strings.ContainsAny(name, "\\\x00")
}
