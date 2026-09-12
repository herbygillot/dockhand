package macports

import (
	"errors"
	"io/fs"
	"maps"
	"path/filepath"

	"github.com/herbygillot/dockhand/v2/internal/record"
)

type Context struct {
	source   record.Source
	root     string
	target   record.Target
	platform record.Platform
}

func NewContext(source record.Source, root string, target record.Target, platform record.Platform) (Context, error) {
	if source.Tree == "" || !filepath.IsAbs(root) || target.Name == "" || !fs.ValidPath(target.Portfile) {
		return Context{}, errors.New("macports: source tree, absolute root, and selected target are required")
	}
	target.Variants = maps.Clone(target.Variants)
	return Context{source: source, root: root, target: target, platform: platform}, nil
}

func (c Context) Source() record.Source     { return c.source }
func (c Context) Root() string              { return c.root }
func (c Context) Platform() record.Platform { return c.platform }
func (c Context) Target() record.Target {
	target := c.target
	target.Variants = maps.Clone(target.Variants)
	return target
}
