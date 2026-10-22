package engine

import (
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
)

// Ports are what the engine reads of ports, a field for each capability,
// where it found each by type assertion on one reader, and did without
// one it didn't find, unsaid (C3). With no Reader, every one is MacPorts'
// own (treequery.Reader), which has them all; given a Reader, as a test
// does, only the fields set are, and one missing is said where it's
// needed, or is an error.
type Ports struct {
	// Reader reads ports for plans.
	Reader PortReader
	// Families make change records.
	Families FamilyReader
	// Graphs order a plan's builds through ports it doesn't build, and
	// find a baseline's and reuse's dependencies on.
	Graphs DependencyGraphReader
	// Archives say what a port fetches, for its assessment.
	Archives ArchivePlanner
	// Fetcher fetches the archives a Portfile declares, for diff
	// --archives.
	Fetcher ArchiveFetcher
	// Names name the ports a directory defines, as the port index has
	// them.
	Names PortNamer
	// Dependents find a port's dependents, for impact and review.
	Dependents DependentReader
	// Maintainers find how the port index spells a maintainer.
	Maintainers MaintainerReader
}

// MacPortsPorts are MacPorts' own reader's capabilities, every one.
func MacPortsPorts(reader *treequery.Reader) Ports {
	return Ports{Reader: reader, Families: reader, Graphs: reader, Archives: reader, Fetcher: reader, Names: reader, Dependents: reader, Maintainers: reader}
}

// capabilities are the engine's Ports: those given, or MacPorts' own where
// no Reader was.
func (e *Engine) capabilities() (Ports, error) {
	e.lazy.Lock()
	given := e.Ports
	e.lazy.Unlock()
	if given.Reader != nil {
		return given, nil
	}
	reader, err := assemble(e, &e.macPorts, func() (*treequery.Reader, error) {
		ports, err := e.selectionReader()
		if err != nil {
			return nil, err
		}
		return treequery.New(e.Repo, ports, &workspace.Registry{}), nil
	})
	if err != nil {
		return Ports{}, err
	}
	own := MacPortsPorts(reader)
	// A capability given beside no reader stands in for MacPorts', as a
	// test's archive planner does.
	if given.Families != nil {
		own.Families = given.Families
	}
	if given.Graphs != nil {
		own.Graphs = given.Graphs
	}
	if given.Archives != nil {
		own.Archives = given.Archives
	}
	if given.Fetcher != nil {
		own.Fetcher = given.Fetcher
	}
	if given.Names != nil {
		own.Names = given.Names
	}
	if given.Dependents != nil {
		own.Dependents = given.Dependents
	}
	if given.Maintainers != nil {
		own.Maintainers = given.Maintainers
	}
	return own, nil
}

// portReader is the engine's port reader (Ports.Reader).
func (e *Engine) portReader() (PortReader, error) {
	ports, err := e.capabilities()
	return ports.Reader, err
}
