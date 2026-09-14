package macports

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
)

type Tree struct {
	source   record.Source
	root     string
	platform record.Platform
}

func NewTree(source record.Source, root string, platform record.Platform) (Tree, error) {
	if source.Tree == "" || !filepath.IsAbs(root) {
		return Tree{}, fmt.Errorf("macports: source tree and absolute snapshot root are required")
	}
	root, err := filepath.EvalSymlinks(root)
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
	return Tree{source: source, root: root, platform: platform}, nil
}

func (t Tree) Source() record.Source     { return t.source }
func (t Tree) Root() string              { return t.root }
func (t Tree) Platform() record.Platform { return t.platform }

type Context struct {
	Tree
	target record.Target
}

func NewContext(source record.Source, root string, target record.Target, platform record.Platform) (Context, error) {
	tree, err := NewTree(source, root, platform)
	if err != nil {
		return Context{}, err
	}
	return tree.Select(target)
}

func (t Tree) Select(target record.Target) (Context, error) {
	if t.root == "" || !token(target.Name) || (target.Subport != "" && !token(target.Subport)) || !portfilePath(target.Portfile) {
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
