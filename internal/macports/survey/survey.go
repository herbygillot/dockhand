package survey

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"path"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portindex"
	portselection "github.com/herbygillot/dockhand/internal/macports/selection"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/record"
)

// Selection chooses explicit ports, exact maintainer/category filters, or all ports.
type Selection struct {
	All            bool
	Ports          []string
	Maintainers    []string
	Categories     []string
	NotMaintainers []string
}

// Validate rejects mixed selector modes and malformed filter values.
func (s Selection) Validate() error {
	grouped := s.All || len(s.Maintainers)+len(s.Categories)+len(s.NotMaintainers) > 0
	if grouped && len(s.Ports) > 0 {
		return fmt.Errorf("choose explicit ports, maintainer/category filters, or --all; do not mix selector modes")
	}
	if grouped {
		return (portindex.Filter{All: s.All, Maintainers: s.Maintainers, Categories: s.Categories, NotMaintainers: s.NotMaintainers}).Validate()
	}
	if len(s.Ports) == 0 {
		return fmt.Errorf("select ports with arguments or a selection flag")
	}
	for _, port := range s.Ports {
		if strings.TrimSpace(port) == "" {
			return fmt.Errorf("port must not be empty")
		}
	}
	return nil
}

// Port retains an indexed name when selection came from metadata.
type Port struct {
	Label     string
	Name      string
	Selection macports.Selection
	// Portfile is the file the selection resolves to when the index knows
	// it, "category/port/Portfile"; ports that share one must not be
	// probed at the same time, since probing writes candidates into it.
	Portfile string
}

// Workspace holds the whole tree at HEAD until Close. Probes overlay its
// Projection for their candidates and never write into it.
type Workspace struct {
	Source   record.Source
	Root     string
	Ports    []Port
	Problems []portindex.SelectionProblem
	// Projection is the workspace the tree is materialized in.
	Projection *workspace.Workspace
	release    func() error
}

func (w *Workspace) Close() error { return w.release() }

// Open freezes HEAD, then selects explicit ports or stages and queries its
// index. The tree comes from the registry when one is given. Explicit
// names need no index source, only a filter does; with one, explicit names
// that share a Portfile are grouped.
func Open(ctx context.Context, repo *git.Repository, workspaces *workspace.Registry, platform record.Platform, index portindex.Source, selection Selection) (_ *Workspace, err error) {
	return OpenAt(ctx, repo, "HEAD", workspaces, platform, index, selection)
}

// OpenAt is Open for a named revision, such as a freshly fetched master,
// rather than HEAD.
func OpenAt(ctx context.Context, repo *git.Repository, revision string, workspaces *workspace.Registry, platform record.Platform, index portindex.Source, selection Selection) (_ *Workspace, err error) {
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	commit, err := repo.Resolve(ctx, revision+"^{commit}")
	if err != nil {
		return nil, err
	}
	trees, err := repo.CommitTrees(ctx, []string{commit})
	if err != nil {
		return nil, err
	}
	source := record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(trees[commit])}
	files, release, err := workspaces.Acquire(ctx, repo, source)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, release())
		}
	}()
	// A survey resolves names and evaluates whatever it selected: the
	// whole tree.
	if err := files.EnsureAll(ctx); err != nil {
		return nil, err
	}
	if err := macports.ValidatePortsTree(files.Root(), repo.Root); err != nil {
		return nil, err
	}
	into, err := files.Tree(platform)
	if err != nil {
		return nil, err
	}
	ports, problems, err := selectPorts(ctx, index, into, selection)
	if err != nil {
		return nil, err
	}
	return &Workspace{Source: source, Root: files.Root(), Ports: ports, Problems: problems, Projection: files, release: release}, nil
}

func selectPorts(ctx context.Context, source portindex.Source, into macports.Tree, selection Selection) ([]Port, []portindex.SelectionProblem, error) {
	var selected []Port
	var problems []portindex.SelectionProblem
	seen := map[string]bool{}
	if len(selection.Ports) > 0 {
		// The index is staged for resolution later anyway; consulting it now
		// tells which explicit names share a Portfile.
		var index *portindex.Index
		if source != nil {
			var err error
			if index, err = source.Index(ctx, into); err != nil {
				progress.VerboseReport(ctx, "PortIndex unavailable for grouping explicit ports: %v", err)
				index = nil
			}
		}
		for _, selector := range selection.Ports {
			if seen[selector] {
				continue
			}
			seen[selector] = true
			port := Port{Label: selector, Selection: macports.Selection{Selector: selector}}
			if strings.Contains(selector, "/") {
				port.Portfile = selector
			} else if index != nil {
				if entry, err := index.Lookup(selector); err == nil {
					port.Portfile = path.Join(entry.Portdir, "Portfile")
				}
			}
			selected = append(selected, port)
		}
	}
	if len(selection.Ports) == 0 {
		if source == nil {
			return nil, nil, fmt.Errorf("survey: selecting ports by filter needs an index source")
		}
		index, err := source.Index(ctx, into)
		if err != nil {
			return nil, nil, err
		}
		matches, err := index.Select(ctx, portindex.Filter{All: selection.All, Maintainers: selection.Maintainers, Categories: selection.Categories, NotMaintainers: selection.NotMaintainers})
		if err != nil {
			return nil, nil, err
		}
		for _, entry := range matches.Entries {
			selected = append(selected, Port{Label: entry.Name, Name: entry.Name, Selection: portselection.FromEntry(entry, nil), Portfile: path.Join(entry.Portdir, "Portfile")})
		}
		problems = matches.Problems
		progress.VerboseReport(ctx, "Selected %d indexed ports; %d selection coverage problems", len(selected), len(matches.Problems))
	}
	return selected, problems, nil
}
