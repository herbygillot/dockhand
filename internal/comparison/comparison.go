// Package comparison collects what upstream's change means for a port:
// from each version's fetch plan or Git source, it reads the archives,
// pairs them by the port's source set, checks the port's patches against
// the new source, observes the ports its requirements name, and applies
// the assessment's rules (macports/assess), for a revision's assessment
// and an update's alike (Then's item 15, C8-7; the architecture thread's
// shape, 2026-10-27). It reads no store and no Git repository: what it
// fetches and evaluates goes through the interfaces its Collector is
// given, and the engine records what it returns.
package comparison

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/assess"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/project"
)

// Collector collects upstream comparisons through what it's given.
type Collector struct {
	// Planner evaluates a port of a directory, and the archives its fetch
	// plan names.
	Planner ArchivePlanner
	// Archiver writes a Git-fetched port's forge archive of a commit; nil
	// where none can be had, which ArchiverProblem says.
	Archiver        SourceArchiver
	ArchiverProblem error
	// Resolver resolves a Git source's ref to its commit now.
	Resolver Resolver
	// Files reads a tree's files, as a port's patches.
	Files Files
	// Observer evaluates the ports an assessment's requirements name.
	Observer Observer
	// Mirror is MacPorts' distfiles mirror, its own where empty, and
	// Readings where readings of archives are kept.
	Mirror   string
	Readings project.Cache
}

// ArchivePlanner evaluates a port of a directory, or its first where port
// is empty, and the archives its fetch plan names: its own, where it
// declares crates or Go modules too; treequery.ErrNoArchives where it
// names none, as for a port fetched with Git, or a metaport.
type ArchivePlanner interface {
	ArchivePlan(ctx context.Context, source model.Source, directory, port string) (macports.PortInfo, []macports.Distfile, error)
}

// Resolver resolves a Git source's ref to the commit it names now; one
// that can't be resolved says why (model.GitSource's Missing, Unresolved,
// or an Abbreviation only a clone expands).
type Resolver interface {
	Resolve(ctx context.Context, source model.GitSource) model.GitSource
}

// Files reads one file of a tree; false where the tree has none.
type Files interface {
	File(ctx context.Context, tree model.ObjectID, path string) ([]byte, bool, error)
}

// Observer evaluates a port of a tree for an assessment's observation of
// it (assess.Wanted): its version, its directory, and its Python
// projects, or that the tree has none, or why it couldn't be read.
type Observer interface {
	Observe(ctx context.Context, tree model.Source, port string) assess.Observation
}

// archiver is the Archiver, or why there's none.
func (c Collector) archiver() (SourceArchiver, error) {
	if c.Archiver == nil {
		if c.ArchiverProblem != nil {
			return nil, c.ArchiverProblem
		}
		return nil, errors.New("no forge archiver is set up")
	}
	return c.Archiver, nil
}
