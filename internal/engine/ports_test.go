package engine

// usePorts gives the engine reader as its port reader, with each
// capability the reader has, leaving any other given apart: a test's fake
// says by its methods what it reads, where the engine found that by type
// assertion before C3.
func usePorts(e *Engine, reader PortReader) {
	e.Ports.Reader = reader
	if families, ok := reader.(FamilyReader); ok {
		e.Ports.Families = families
	}
	if graphs, ok := reader.(DependencyGraphReader); ok {
		e.Ports.Graphs = graphs
	}
	if planner, ok := reader.(ArchivePlanner); ok {
		e.Ports.Archives = planner
	}
	if fetcher, ok := reader.(ArchiveFetcher); ok {
		e.Ports.Fetcher = fetcher
	}
	if names, ok := reader.(PortNamer); ok {
		e.Ports.Names = names
	}
	if dependents, ok := reader.(DependentReader); ok {
		e.Ports.Dependents = dependents
	}
	if maintainers, ok := reader.(MaintainerReader); ok {
		e.Ports.Maintainers = maintainers
	}
}

// useDependents gives the engine reader as what finds dependents, and as
// what names a directory's ports where it does that too.
func useDependents(e *Engine, reader DependentReader) {
	e.Ports.Dependents = reader
	if names, ok := reader.(PortNamer); ok {
		e.Ports.Names = names
	}
}
