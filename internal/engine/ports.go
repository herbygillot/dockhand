package engine

import (
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
)

// portReader is the engine's PortReader: the one it was given, or
// MacPorts' own evaluator.
func (e *Engine) portReader() (PortReader, error) {
	return assemble(e, &e.PortReader, func() (PortReader, error) {
		ports, err := e.selectionReader()
		if err != nil {
			return nil, err
		}
		return treequery.New(e.Repo, ports, &workspace.Registry{}), nil
	})
}
