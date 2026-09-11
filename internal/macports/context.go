package macports

import (
	"errors"
	"io/fs"
	"maps"
	"path/filepath"

	"github.com/herbygillot/dockhand/v2/internal/model"
)

type Context struct {
	source   model.Source
	root     string
	target   model.Target
	platform model.Platform
}

func NewContext(source model.Source, root string, target model.Target, platform model.Platform) (Context, error) {
	if source.Tree == "" || !filepath.IsAbs(root) || target.Name == "" || !fs.ValidPath(target.Portfile) {
		return Context{}, errors.New("macports: source tree, absolute root, and selected target are required")
	}
	target.Variants = maps.Clone(target.Variants)
	return Context{source: source, root: root, target: target, platform: platform}, nil
}

func (c Context) Source() model.Source     { return c.source }
func (c Context) Root() string             { return c.root }
func (c Context) Platform() model.Platform { return c.platform }
func (c Context) Target() model.Target {
	target := c.target
	target.Variants = maps.Clone(target.Variants)
	return target
}
