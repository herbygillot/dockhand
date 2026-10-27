package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/comparison"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	"github.com/herbygillot/dockhand/internal/macports/treequery"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// SourceArchiver writes a Git-fetched port's forge's archive of a commit,
// for its assessment (comparison.SourceArchiver).
type SourceArchiver = comparison.SourceArchiver

// collector is the comparison's collector over the engine's readers: the
// planner given, the source archiver, Git, the person's checkout's files,
// and the port reader, for a revision's assessment and an update's alike
// (Then's item 15).
func (e *Engine) collector(planner ArchivePlanner) comparison.Collector {
	archiver, problem := e.sourceArchiver()
	c := comparison.Collector{Archiver: archiver, ArchiverProblem: problem, Resolver: gitResolver{e}, Files: treeFiles{e.Repo},
		Mirror: e.ArchiveMirror, Readings: e.readings()}
	if planner != nil {
		c.Planner = planner
	}
	if reader, err := e.portReader(); err == nil {
		c.Observer = portObserver{reader}
	} else {
		c.Observer = unreadablePorts{err}
	}
	return c
}

// readings is where readings of upstream's archives are kept; none are
// where the engine wasn't given a place.
func (e *Engine) readings() project.Cache {
	return project.Cache{Directory: e.options.Readings}
}

// sourceArchiver is the one compose gave the engine, upstream
// discovery's forges (Then's item 15).
func (e *Engine) sourceArchiver() (SourceArchiver, error) {
	if e.SourceArchiver == nil {
		return nil, errors.New("no forge archiver is set up: MacPorts' evaluator, port-tclsh, couldn't be read")
	}
	return e.SourceArchiver, nil
}

// gitResolver resolves a Git source's ref as a check's plan does
// (resolveGit).
type gitResolver struct{ e *Engine }

func (g gitResolver) Resolve(ctx context.Context, source model.GitSource) model.GitSource {
	return g.e.resolveGit(ctx, source)
}

// treeFiles reads a tree's files from the person's checkout's objects.
type treeFiles struct{ repo *git.Repository }

func (t treeFiles) File(ctx context.Context, tree model.ObjectID, path string) ([]byte, bool, error) {
	file, data, err := t.repo.File(ctx, string(tree), path)
	return data, err == nil && file.Exists, err
}

// portObserver observes a port of a tree as MacPorts evaluates it on this
// Mac, for an assessment's providers.
type portObserver struct{ reader PortReader }

func (p portObserver) Observe(ctx context.Context, tree model.Source, name string) assess.Observation {
	port, directory, err := providerPort(ctx, p.reader, tree, name)
	switch {
	case errors.Is(err, treequery.ErrNoPort) || errors.Is(err, portindex.ErrNotIndexed):
		return assess.Observation{Absent: true}
	case err != nil:
		return assess.Observation{Problem: err.Error()}
	}
	return assess.Observation{Version: port.Version, Directory: directory, Packages: port.PythonProjects()}
}

// unreadablePorts observes nothing, saying why the ports can't be read.
type unreadablePorts struct{ err error }

func (u unreadablePorts) Observe(context.Context, model.Source, string) assess.Observation {
	return assess.Observation{Problem: "couldn't read the ports: " + u.err.Error()}
}

// providerPort is a port in a tree, as MacPorts evaluates it on this Mac,
// and its directory; ErrNoPort, or the index's ErrNotIndexed, where the
// tree has no such port.
func providerPort(ctx context.Context, reader PortReader, source model.Source, name string) (macports.PortInfo, string, error) {
	directory, err := reader.Directory(ctx, source, name)
	if err != nil {
		return macports.PortInfo{}, "", err
	}
	ports, err := reader.Ports(ctx, source, directory, model.Environment{}, nil)
	if err != nil {
		return macports.PortInfo{}, "", err
	}
	for _, port := range ports {
		if port.Name == name {
			return port, directory, nil
		}
	}
	return macports.PortInfo{}, "", fmt.Errorf("%w: %s defines no port %s", treequery.ErrNoPort, directory, name)
}
